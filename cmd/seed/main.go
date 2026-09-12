// Seed is an explicit local development command. The default plan command never
// loads application configuration, opens a database, sends email or writes files.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/dinhdev-nu/chat-platform-api/config"
	"github.com/dinhdev-nu/chat-platform-api/internal/seed"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout, os.Stderr, time.Now())
	stop()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

type commandOptions struct {
	command        string
	profile        seed.Options
	batch          int
	database       string
	writersStopped bool
	baseline       bool
	timeout        time.Duration
	flags          *flag.FlagSet
}

func run(ctx context.Context, args []string, out, errOut io.Writer, now time.Time) error {
	o, err := parseCommand(args, errOut, now)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if o.command == "plan" {
		return writePlan(out, o.profile)
	}
	if err := o.validateOnline(now); err != nil {
		return err
	}
	return o.execute(ctx, out)
}

func parseCommand(args []string, errOut io.Writer, now time.Time) (*commandOptions, error) {
	command := "plan"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	if command != "plan" && command != "apply" && command != "verify" && command != "sync" && command != "reset" {
		return nil, fmt.Errorf("unknown command %q; use plan, apply, verify, sync or reset", command)
	}
	o := &commandOptions{command: command, profile: seed.Defaults(now)}
	fs := flag.NewFlagSet("seed "+command, flag.ContinueOnError)
	o.flags = fs
	fs.SetOutput(errOut)
	fs.StringVar(&o.profile.Dataset, "dataset", o.profile.Dataset, "dataset name; also identifies an existing seed for verify/sync/reset")
	fs.IntVar(&o.profile.Users, "users", o.profile.Users, "exactly 50 users, including both configured test emails")
	fs.IntVar(&o.profile.Conversations, "conversations", o.profile.Conversations, "50-150 conversations")
	fs.IntVar(&o.profile.Messages, "messages", o.profile.Messages, "25000-50000 messages, including system and soft-deleted messages")
	fs.Uint64Var(&o.profile.RandomSeed, "random-seed", o.profile.RandomSeed, "nonzero reproducible random seed")
	at := fs.String("at", "", "optional RFC3339 whole-second end of generated history")
	fs.IntVar(&o.batch, "batch-size", 500, "1-1000 rows per SQL insert")
	fs.StringVar(&o.database, "database", "", "required for database commands: must match the configured local MySQL database")
	fs.BoolVar(&o.writersStopped, "writers-stopped", false, "acknowledge that API and worker writers are stopped (apply/sync/reset)")
	fs.BoolVar(&o.baseline, "baseline", false, "verify exact original totals before interacting with the seeded application")
	fs.DurationVar(&o.timeout, "timeout", 10*time.Minute, "maximum time for a database command")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(errOut, "Usage: go run ./cmd/seed [plan|apply|verify|sync|reset] [flags]")
		_, _ = fmt.Fprintln(errOut, "Default: offline plan only. Database commands require APP_ENV=local and --database.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if *at != "" {
		parsed, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			return nil, fmt.Errorf("at: %w", err)
		}
		o.profile.At = parsed.UTC()
	}
	if err := o.profile.Validate(); err != nil {
		return nil, err
	}
	if o.batch < 1 || o.batch > 1000 || o.timeout <= 0 || o.timeout > time.Hour {
		return nil, fmt.Errorf("batch-size must be 1-1000 and timeout must be positive and at most 1h")
	}
	return o, nil
}

func writePlan(out io.Writer, options seed.Options) error {
	d, err := seed.Generate(options)
	if err != nil {
		return err
	}
	if err := d.Validate(); err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Mode string `json:"mode"`
		seed.Summary
	}{Mode: "offline plan; no database/Redis connections or writes", Summary: d.Summary()})
}

func (o *commandOptions) validateOnline(now time.Time) error {
	if o.command != "apply" {
		var invalid string
		o.flags.Visit(func(f *flag.Flag) {
			if f.Name == "users" || f.Name == "conversations" || f.Name == "messages" || f.Name == "random-seed" || f.Name == "at" {
				invalid = f.Name
			}
		})
		if invalid != "" {
			return fmt.Errorf("%s reads the saved dataset profile; do not pass --%s", o.command, invalid)
		}
	}
	if o.command != "verify" && !o.writersStopped {
		return fmt.Errorf("stop API and worker writers, then pass --writers-stopped for %s", o.command)
	}
	if o.database == "" || os.Getenv("APP_ENV") != "local" {
		return fmt.Errorf("database commands require explicit APP_ENV=local and --database NAME")
	}
	if o.command == "apply" && o.profile.At.After(now.Add(-30*time.Second)) {
		return fmt.Errorf("history must end at least 30 seconds in the past to keep edit/delete timestamps in the past")
	}
	return nil
}

func (o *commandOptions) execute(ctx context.Context, out io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := seed.CheckTarget(cfg, os.Getenv("APP_ENV"), o.database); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Target: MySQL %s:%d/%s; Redis %s:%d DB %d\n",
		cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.Database, cfg.Redis.Host, cfg.Redis.Port, cfg.Redis.Database); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	store, err := seed.Open(ctx, cfg, o.batch, out)
	if err != nil {
		return err
	}
	defer store.Close()
	switch o.command {
	case "apply":
		d, err := seed.Generate(o.profile)
		if err != nil {
			return err
		}
		return store.Apply(ctx, d)
	case "verify":
		return store.Verify(ctx, o.profile.Dataset, o.baseline)
	case "sync":
		return store.Sync(ctx, o.profile.Dataset)
	case "reset":
		return store.Reset(ctx, o.profile.Dataset)
	}
	return nil
}
