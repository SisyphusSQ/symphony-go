package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/SisyphusSQ/symphony-go/internal/orchestrator"
	"github.com/SisyphusSQ/symphony-go/internal/workflow"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "symphony",
		Short: "Run the Symphony agent orchestration service",
		Long: "Symphony coordinates issue-tracker work, isolated workspaces, and coding agent runs.\n" +
			"This Go port currently exposes the command surface while runtime packages are implemented.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		newRunCommand(),
		newValidateCommand(),
		newNotImplementedCommand("status", "Show orchestrator runtime status"),
		newNotImplementedCommand("pause", "Pause dispatching new issue runs"),
		newNotImplementedCommand("resume", "Resume dispatching issue runs"),
		newNotImplementedCommand("drain", "Stop accepting new work and wait for active runs"),
		newIssueCommand("cancel", "Cancel an active issue run"),
		newIssueCommand("retry", "Retry a failed or stopped issue run"),
		newCleanupCommand(),
		newNotImplementedCommand("doctor", "Check local Symphony dependencies and configuration"),
	)

	return root
}

func newRunCommand() *cobra.Command {
	opts := runOptions{}

	cmd := &cobra.Command{
		Use:   "run [workflow]",
		Short: "Run the Symphony orchestrator",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveWorkflowArg(opts.workflowPath, args)
			if err != nil {
				return err
			}
			if err := workflow.RequireReadable(path); err != nil {
				return fmt.Errorf("startup failed: %w", err)
			}
			runtime, err := orchestrator.NewRuntime(path)
			if err != nil {
				return fmt.Errorf("startup failed: %w", err)
			}

			details := ""
			if opts.port != 0 {
				details += fmt.Sprintf("; server.port override %d", opts.port)
			}
			if opts.instance != "" {
				details += fmt.Sprintf("; instance %q", opts.instance)
			}
			cmd.Printf("workflow %q passed startup validation%s\n", path, details)
			if err := runtime.DispatchReady(); err != nil {
				cmd.Printf("orchestrator runtime loaded; dispatch dependencies are not configured in this CLI slice: %v\n", err)
				return nil
			}
			return runtime.Run(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&opts.workflowPath, "workflow", "", "workflow file path (defaults to ./WORKFLOW.md)")
	cmd.Flags().IntVar(&opts.port, "port", 0, "override server.port from the workflow file")
	cmd.Flags().StringVar(&opts.instance, "instance", "", "operator-defined instance name")

	return cmd
}

func newValidateCommand() *cobra.Command {
	var workflowPath string

	cmd := &cobra.Command{
		Use:   "validate [workflow]",
		Short: "Validate a workflow file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := resolveWorkflowArg(workflowPath, args)
			if err != nil {
				return err
			}
			if err := workflow.RequireReadable(path); err != nil {
				return err
			}
			cmd.Printf("workflow %q passed startup validation\n", path)
			return nil
		},
	}

	cmd.Flags().StringVar(&workflowPath, "workflow", "", "workflow file path (defaults to ./WORKFLOW.md)")

	return cmd
}

func newIssueCommand(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " ISSUE",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(name, fmt.Sprintf("issue %q", args[0]))
		},
	}
}

func newCleanupCommand() *cobra.Command {
	var terminalOnly bool

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean terminal issue workspaces and local runtime state",
		RunE: func(cmd *cobra.Command, args []string) error {
			details := "all configured cleanup targets"
			if terminalOnly {
				details = "terminal issue workspaces only"
			}
			return notImplemented("cleanup", details)
		},
	}

	cmd.Flags().BoolVar(&terminalOnly, "terminal", false, "clean only terminal issue workspaces")

	return cmd
}

func newNotImplementedCommand(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return notImplemented(name, "")
		},
	}
}

func resolveWorkflowArg(flagValue string, args []string) (string, error) {
	if flagValue != "" && len(args) > 0 {
		return "", errors.New("provide the workflow path either as an argument or with --workflow, not both")
	}
	if flagValue != "" {
		return workflow.ResolvePath(flagValue), nil
	}
	if len(args) > 0 {
		return workflow.ResolvePath(args[0]), nil
	}
	return workflow.ResolvePath(""), nil
}

func notImplemented(command string, details string) error {
	if details == "" {
		return fmt.Errorf("%s is not implemented yet", command)
	}
	return fmt.Errorf("%s is not implemented yet (%s)", command, details)
}

type runOptions struct {
	workflowPath string
	port         int
	instance     string
}
