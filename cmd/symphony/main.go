package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/orchestrator"
	httpserver "github.com/SisyphusSQ/symphony-go/internal/server"
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
		newHTTPCommand("status", "Show orchestrator runtime status", http.MethodGet, "/status"),
		newHTTPCommand("pause", "Pause dispatching new issue runs", http.MethodPost, "/orchestrator/pause"),
		newHTTPCommand("resume", "Resume dispatching issue runs", http.MethodPost, "/orchestrator/resume"),
		newHTTPCommand("drain", "Stop accepting new work and wait for active runs", http.MethodPost, "/orchestrator/drain"),
		newIssueCommand("cancel", "Cancel an active issue run", "cancel"),
		newIssueCommand("retry", "Retry a failed or stopped issue run", "retry"),
		newCleanupCommand(),
		newHTTPCommand("doctor", "Check local Symphony dependencies and configuration", http.MethodGet, "/doctor"),
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
			def, err := workflow.Load(path)
			if err != nil {
				return fmt.Errorf("startup failed: %w", err)
			}
			cfg, err := config.FromWorkflow(def)
			if err != nil {
				return fmt.Errorf("startup failed: %w", err)
			}
			runtime, err := orchestrator.NewRuntime(path)
			if err != nil {
				return fmt.Errorf("startup failed: %w", err)
			}
			serverOpts, err := resolveServerOptions(
				cfg,
				workflowServerPortConfigured(def),
				opts.port,
				cmd.Flags().Changed("port"),
				opts.instance,
			)
			if err != nil {
				return err
			}

			details := ""
			if serverOpts.Enabled {
				details += fmt.Sprintf("; server.port %d from %s", serverOpts.Port, serverOpts.Source)
			}
			if opts.instance != "" {
				details += fmt.Sprintf("; instance %q", opts.instance)
			}
			cmd.Printf("workflow %q passed startup validation%s\n", path, details)
			var shutdown func(context.Context) error
			if serverOpts.Enabled {
				listenURL, closeServer, err := startOperatorHTTPServer(cmd.Context(), runtime, serverOpts)
				if err != nil {
					return fmt.Errorf("operator HTTP server failed: %w", err)
				}
				shutdown = closeServer
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_ = shutdown(ctx)
				}()
				cmd.Printf("operator HTTP server listening on %s\n", listenURL)
			}
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

func newHTTPCommand(name, short string, method string, path string) *cobra.Command {
	opts := httpCommandOptions{endpoint: defaultOperatorEndpoint()}
	cmd := &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runOperatorRequest(cmd, method, opts.endpoint, path)
		},
	}
	cmd.Flags().StringVar(&opts.endpoint, "endpoint", opts.endpoint, "operator HTTP endpoint")
	return cmd
}

func newIssueCommand(name, short string, action string) *cobra.Command {
	opts := httpCommandOptions{endpoint: defaultOperatorEndpoint()}
	cmd := &cobra.Command{
		Use:   name + " ISSUE",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/runs/" + url.PathEscape(args[0]) + "/" + action
			return runOperatorRequest(cmd, http.MethodPost, opts.endpoint, path)
		},
	}
	cmd.Flags().StringVar(&opts.endpoint, "endpoint", opts.endpoint, "operator HTTP endpoint")
	return cmd
}

func newCleanupCommand() *cobra.Command {
	var terminalOnly bool
	opts := httpCommandOptions{endpoint: defaultOperatorEndpoint()}

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean terminal issue workspaces and local runtime state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !terminalOnly {
				return errors.New("only cleanup --terminal is supported by the operator HTTP surface")
			}
			return runOperatorRequest(cmd, http.MethodPost, opts.endpoint, "/orchestrator/cleanup?terminal=true")
		},
	}

	cmd.Flags().BoolVar(&terminalOnly, "terminal", false, "clean only terminal issue workspaces")
	cmd.Flags().StringVar(&opts.endpoint, "endpoint", opts.endpoint, "operator HTTP endpoint")

	return cmd
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

type runOptions struct {
	workflowPath string
	port         int
	instance     string
}

type serverOptions struct {
	Enabled  bool
	BindHost string
	Port     int
	Source   string
	Instance string
}

type httpCommandOptions struct {
	endpoint string
}

func resolveServerOptions(
	cfg config.Config,
	workflowPortConfigured bool,
	cliPort int,
	cliPortSet bool,
	instance string,
) (serverOptions, error) {
	opts := serverOptions{
		BindHost: httpserver.DefaultBindHost,
		Instance: instance,
	}
	if cliPortSet {
		if err := validatePort(cliPort); err != nil {
			return serverOptions{}, err
		}
		opts.Enabled = true
		opts.Port = cliPort
		opts.Source = "cli"
		return opts, nil
	}
	if workflowPortConfigured {
		opts.Enabled = true
		opts.Port = cfg.Server.Port
		opts.Source = "workflow"
		return opts, nil
	}
	return opts, nil
}

func validatePort(port int) error {
	if port < 0 || port > config.MaxServerPort {
		return fmt.Errorf("server.port must be between 0 and %d", config.MaxServerPort)
	}
	return nil
}

func workflowServerPortConfigured(def workflow.Definition) bool {
	serverConfig, ok := def.Config["server"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = serverConfig["port"]
	return ok
}

func startOperatorHTTPServer(
	ctx context.Context,
	runtime *orchestrator.Runtime,
	opts serverOptions,
) (string, func(context.Context) error, error) {
	addr := net.JoinHostPort(opts.BindHost, strconv.Itoa(opts.Port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{
		Handler: httpserver.NewHandler(runtime, httpserver.Config{
			BindHost: opts.BindHost,
			Port:     opts.Port,
			Instance: opts.Instance,
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		err := srv.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "operator HTTP server stopped: %v\n", err)
		}
	}()
	return "http://" + listener.Addr().String(), srv.Shutdown, nil
}

func runOperatorRequest(cmd *cobra.Command, method string, endpoint string, path string) error {
	base, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil {
		return fmt.Errorf("invalid operator endpoint: %w", err)
	}
	rel, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("invalid operator path: %w", err)
	}
	target := base.ResolveReference(rel)
	req, err := http.NewRequestWithContext(cmd.Context(), method, target.String(), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return readErr
	}
	if len(data) > 0 {
		cmd.Print(string(data))
		if data[len(data)-1] != '\n' {
			cmd.Println()
		}
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("operator request failed: %s", resp.Status)
	}
	return nil
}

func defaultOperatorEndpoint() string {
	if endpoint := strings.TrimSpace(os.Getenv("SYMPHONY_OPERATOR_ENDPOINT")); endpoint != "" {
		return endpoint
	}
	return "http://127.0.0.1:4002"
}
