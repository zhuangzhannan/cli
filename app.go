package cli

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"time"
)

// Tries to find out when this binary was compiled.
// Returns the current time if it fails to find it.
func compileTime() time.Time {
	info, err := os.Stat(os.Args[0])
	if err != nil {
		return time.Now()
	}
	return info.ModTime()
}

// Run is the entry point to the cli app. Parses the arguments slice and routes
// to the proper flag/args combination
func (a *App) Run(arguments []string) (err error) {
	a.Setup()

	// parse flags
	set := flagSet(a.Name, a.Flags)
	set.SetOutput(ioutil.Discard)
	err = set.Parse(arguments[1:])
	nerr := normalizeFlags(a.Flags, set)
	context := NewContext(a, set, nil)
	if nerr != nil {
		fmt.Fprintln(a.Writer, nerr)
		ShowAppHelp(context)
		return nerr
	}

	if checkCompletions(context) {
		return nil
	}

	if err != nil {
		if a.OnUsageError != nil {
			err := a.OnUsageError(context, err, false)
			HandleExitCoder(err)
			return err
		}
		fmt.Fprintf(a.Writer, "Incorrect Usage: %s\n\n", err)
		ShowAppHelp(context)
		return err
	}

	if !a.HideHelp && checkHelp(context) {
		ShowAppHelp(context)
		return nil
	}

	if !a.HideVersion && checkVersion(context) {
		ShowVersion(context)
		return nil
	}

	if a.After != nil {
		defer func() {
			if afterErr := a.After(context); afterErr != nil {
				if err != nil {
					err = newMultiError(err, afterErr)
				} else {
					err = afterErr
				}
			}
		}()
	}

	if a.Before != nil {
		beforeErr := a.Before(context)
		if beforeErr != nil {
			fmt.Fprintf(a.Writer, "%v\n\n", beforeErr)
			ShowAppHelp(context)
			HandleExitCoder(beforeErr)
			err = beforeErr
			return err
		}
	}

	args := context.Args()
	if args.Present() {
		name := args.First()
		c := a.Subcommand(name)
		if c != nil {
			return c.Run(context)
		}
	}

	// Run default Action
	err = a.Action(context)

	HandleExitCoder(err)
	return err
}

// RunAsSubcommand invokes the subcommand given the context, parses ctx.Args() to
// generate command-specific flags
func (a *App) RunAsSubcommand(ctx *Context) (err error) {
	// append help to commands
	if len(a.Subcommands) > 0 {
		if a.Subcommand(helpCommand.Name) == nil && !a.HideHelp {
			a.appendCommand(helpCommand)

			if HelpFlag != nil {
				a.appendFlag(HelpFlag)
			}
		}
	}

	newCmds := []*Command{}
	for _, c := range a.Subcommands {
		if c.HelpName == "" {
			c.HelpName = fmt.Sprintf("%s %s", a.HelpName, c.Name)
		}
		newCmds = append(newCmds, c)
	}
	a.Subcommands = newCmds

	// append flags
	if a.EnableBashCompletion {
		a.appendFlag(BashCompletionFlag)
	}

	// parse flags
	set := flagSet(a.Name, a.Flags)
	set.SetOutput(ioutil.Discard)
	err = set.Parse(ctx.Args().Tail())
	nerr := normalizeFlags(a.Flags, set)
	context := NewContext(a, set, ctx)

	if nerr != nil {
		fmt.Fprintln(a.Writer, nerr)
		fmt.Fprintln(a.Writer)
		if len(a.Subcommands) > 0 {
			ShowSubcommandHelp(context)
		} else {
			ShowCommandHelp(ctx, context.Args().First())
		}
		return nerr
	}

	if checkCompletions(context) {
		return nil
	}

	if err != nil {
		if a.OnUsageError != nil {
			err = a.OnUsageError(context, err, true)
			HandleExitCoder(err)
			return err
		}
		fmt.Fprintf(a.Writer, "Incorrect Usage: %s\n\n", err)
		ShowSubcommandHelp(context)
		return err
	}

	if len(a.Subcommands) > 0 {
		if checkSubcommandHelp(context) {
			return nil
		}
	} else {
		if checkCommandHelp(ctx, context.Args().First()) {
			return nil
		}
	}

	if a.After != nil {
		defer func() {
			afterErr := a.After(context)
			if afterErr != nil {
				HandleExitCoder(err)
				if err != nil {
					err = newMultiError(err, afterErr)
				} else {
					err = afterErr
				}
			}
		}()
	}

	if a.Before != nil {
		beforeErr := a.Before(context)
		if beforeErr != nil {
			HandleExitCoder(beforeErr)
			err = beforeErr
			return err
		}
	}

	args := context.Args()
	if args.Present() {
		name := args.First()
		c := a.Subcommand(name)
		if c != nil {
			return c.Run(context)
		}
	}

	// Run default Action
	err = a.Action(context)

	HandleExitCoder(err)
	return err
}

// Command returns the named command on App. Returns nil if the command does not exist
func (a *App) Subcommand(name string) *Command {
	for _, c := range a.Subcommands {
		if c.HasName(name) {
			return c
		}
	}

	return nil
}

// VisibleCategories returns a slice of categories and commands that are
// Hidden=false
func (a *App) VisibleCategories() []CommandCategory {
	ret := []CommandCategory{}
	for _, category := range a.Categories.Categories() {
		if visible := func() CommandCategory {
			if len(category.VisibleCommands()) > 0 {
				return category
			}
			return nil
		}(); visible != nil {
			ret = append(ret, visible)
		}
	}
	return ret
}

// VisibleCommands returns a slice of the Commands with Hidden=false
func (a *App) VisibleCommands() []*Command {
	ret := []*Command{}
	for _, command := range a.Subcommands {
		if !command.Hidden {
			ret = append(ret, command)
		}
	}
	return ret
}

// VisibleFlags returns a slice of the Flags with Hidden=false
func (a *App) VisibleFlags() []Flag {
	return visibleFlags(a.Flags)
}

func (a *App) hasFlag(flag Flag) bool {
	for _, f := range a.Flags {
		if reflect.DeepEqual(flag, f) {
			return true
		}
	}

	return false
}

func (a *App) errWriter() io.Writer {

	// When the app ErrWriter is nil use the package level one.
	if a.ErrWriter == nil {
		return ErrWriter
	}

	return a.ErrWriter
}

func (a *App) appendFlag(fl Flag) {
	if !hasFlag(a.Flags, fl) {
		a.Flags = append(a.Flags, fl)
	}
}

func (a *App) appendCommand(c *Command) {
	if !hasCommand(a.Subcommands, c) {
		a.Subcommands = append(a.Subcommands, c)
	}
}

// Author represents someone who has contributed to a cli project.
type Author struct {
	Name  string // The Authors name
	Email string // The Authors email
}

// String makes Author comply to the Stringer interface, to allow an easy print in the templating process
func (a *Author) String() string {
	e := ""
	if a.Email != "" {
		e = "<" + a.Email + "> "
	}

	return fmt.Sprintf("%v %v", a.Name, e)
}
