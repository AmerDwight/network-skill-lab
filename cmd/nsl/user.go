package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/AmerDwight/network-skill-lab/internal/auth"
	"github.com/AmerDwight/network-skill-lab/internal/config"
	"github.com/AmerDwight/network-skill-lab/internal/store"
	"golang.org/x/term"
)

const userTimeLayout = "2006-01-02 15:04"

func userUsage() {
	fmt.Fprint(os.Stderr, `usage: nsl user <command>

commands:
  add <name> [--role admin|user] [--password-stdin]   create a user
  passwd <name> [--password-stdin]                    set a user's password
  disable <name>                                      block a user from logging in
  enable <name>                                       let a disabled user log in again
  list                                                list all users
`)
}

func userCmd(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		userUsage()
		return errors.New("missing user command")
	}

	switch args[0] {
	case "add":
		return userAdd(args[1:], stdin, stdout)
	case "passwd":
		return userPasswd(args[1:], stdin, stdout)
	case "disable":
		return userSetDisabled(args[1:], stdout, true)
	case "enable":
		return userSetDisabled(args[1:], stdout, false)
	case "list":
		return userList(args[1:], stdout)
	default:
		userUsage()
		return fmt.Errorf("unknown user command %q", args[0])
	}
}

func userAdd(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("user add", flag.ContinueOnError)
	role := flags.String("role", store.RoleUser, "role of the new user: admin or user")
	fromStdin := flags.Bool("password-stdin", false, "read the password from stdin instead of the terminal")
	username, err := parseUserArgs(flags, args, "nsl user add <name>")
	if err != nil {
		return err
	}
	if *role != store.RoleAdmin && *role != store.RoleUser {
		return fmt.Errorf("unknown role %q, want admin or user", *role)
	}
	if err := auth.ValidateUsername(username); err != nil {
		return err
	}

	password, err := readPassword(stdin, *fromStdin, "password: ")
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	st, err := openUserStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	user := store.User{ID: store.NewID(), Username: username, PasswordHash: hash, Role: *role}
	if err := st.Users.Create(context.Background(), user); err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			return fmt.Errorf("user %q already exists", username)
		}
		return err
	}
	_, err = fmt.Fprintf(stdout, "created user %s with role %s\n", username, *role)
	return err
}

func userPasswd(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("user passwd", flag.ContinueOnError)
	fromStdin := flags.Bool("password-stdin", false, "read the password from stdin instead of the terminal")
	username, err := parseUserArgs(flags, args, "nsl user passwd <name>")
	if err != nil {
		return err
	}

	password, err := readPassword(stdin, *fromStdin, "new password: ")
	if err != nil {
		return err
	}
	if err := auth.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}

	st, err := openUserStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	user, err := findUser(st, username)
	if err != nil {
		return err
	}
	if err := st.Users.SetPasswordHash(context.Background(), user.ID, hash); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "changed the password of %s\n", username)
	return err
}

func userSetDisabled(args []string, stdout io.Writer, disabled bool) error {
	name := "user enable"
	if disabled {
		name = "user disable"
	}
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	username, err := parseUserArgs(flags, args, "nsl "+name+" <name>")
	if err != nil {
		return err
	}

	st, err := openUserStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	user, err := findUser(st, username)
	if err != nil {
		return err
	}
	if err := st.Users.SetDisabled(context.Background(), user.ID, disabled); err != nil {
		return err
	}
	action := "enabled"
	if disabled {
		action = "disabled"
	}
	_, err = fmt.Fprintf(stdout, "%s user %s\n", action, username)
	return err
}

func userList(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("user list", flag.ContinueOnError)
	names, err := parseFlags(flags, args)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("usage: nsl user list, got %q", names[0])
	}

	st, err := openUserStore()
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	users, err := st.Users.List(context.Background())
	if err != nil {
		return err
	}

	writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "USERNAME\tROLE\tCREATED\tDISABLED")
	for _, user := range users {
		disabled := "-"
		if user.DisabledAt != nil {
			disabled = user.DisabledAt.Local().Format(userTimeLayout)
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\n", user.Username, user.Role, user.CreatedAt.Local().Format(userTimeLayout), disabled)
	}
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("write user table: %w", err)
	}
	return nil
}

func parseUserArgs(flags *flag.FlagSet, args []string, usage string) (string, error) {
	names, err := parseFlags(flags, args)
	if err != nil {
		return "", err
	}
	if len(names) != 1 {
		return "", fmt.Errorf("usage: %s", usage)
	}
	return names[0], nil
}

func parseFlags(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
		if len(args) == 0 {
			break
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
	return positional, nil
}

func findUser(st *store.Store, username string) (store.User, error) {
	user, err := st.Users.ByUsername(context.Background(), username)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, fmt.Errorf("no such user %q", username)
	}
	if err != nil {
		return store.User{}, err
	}
	return user, nil
}

func openUserStore() (*store.Store, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	return st, nil
}

func readPassword(stdin io.Reader, fromStdin bool, prompt string) (string, error) {
	if fromStdin {
		data, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}

	file, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return "", errors.New("no terminal to read the password from, use --password-stdin")
	}

	first, err := promptPassword(file, prompt)
	if err != nil {
		return "", err
	}
	second, err := promptPassword(file, "repeat password: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("the two passwords do not match")
	}
	return first, nil
}

func promptPassword(file *os.File, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	data, err := term.ReadPassword(int(file.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(data), nil
}
