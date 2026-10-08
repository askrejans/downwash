package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/askrejans/downwash/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/askrejans/downwash/internal/pipeline"
	"github.com/askrejans/downwash/internal/tui"
)

var version = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := newCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	var opts pipeline.Options
	var noTUI, verbose, recursive bool
	logger := func() *slog.Logger {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	}
	runFile := func(ctx context.Context, input string) error {
		opts.InputPath, opts.Logger = input, logger()
		if opts.OutputDir == "" {
			opts.OutputDir = filepath.Dir(input)
		}
		result, err := pipeline.Run(ctx, opts)
		if err != nil {
			return err
		}
		for _, path := range []string{result.GPXPath, result.AltPNGPath, result.TrackPNGPath, result.MarkdownPath, result.MetadataPath, result.PDFPath, result.ZipPath, result.VideoPath} {
			if path != "" {
				fmt.Fprintln(os.Stdout, path)
			}
		}
		return nil
	}
	runBatch := func(ctx context.Context, dir string) error {
		files, err := pipeline.FindVideos(dir, recursive)
		if err != nil {
			return fmt.Errorf("downwash: find videos: %w", err)
		}
		if len(files) == 0 {
			return fmt.Errorf("downwash: no MP4 videos in %s", dir)
		}
		if opts.OutputDir == "" {
			opts.OutputDir = filepath.Join(dir, "processed")
		}
		var failures []error
		for _, file := range files {
			if err := runFile(ctx, file); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", file, err))
			}
		}
		return errors.Join(failures...)
	}
	runInteractive := func(input string) error {
		opts.Logger = logger()
		_, err := tea.NewProgram(tui.New(tui.Config{Version: version, FilePath: input, PipelineOpts: opts})).Run()
		return err
	}
	root := &cobra.Command{Use: "downwash [file.MP4|file.SRT|directory]", Short: "Extract DJI telemetry and create post-flight reports", Args: cobra.MaximumNArgs(1), SilenceUsage: true, SilenceErrors: true}
	root.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			if noTUI || !term.IsTerminal(int(os.Stdin.Fd())) {
				return cmd.Help()
			}
			return runInteractive("")
		}
		info, err := os.Stat(args[0])
		if err != nil {
			return err
		}
		if info.IsDir() {
			return runBatch(cmd.Context(), args[0])
		}
		return runFile(cmd.Context(), args[0])
	}
	flags := root.PersistentFlags()
	flags.StringVarP(&opts.OutputDir, "output", "o", "", "Directory for output artifacts")
	flags.BoolVar(&opts.Transcode, "transcode", false, "Re-encode video using ffmpeg")
	flags.StringVar(&opts.TranscodeCodec, "codec", "h264", "Transcode codec: h264 or h265")
	flags.StringVar(&opts.TranscodeBitrate, "bitrate", "15M", "Target video bitrate")
	flags.StringVar(&opts.TranscodePreset, "preset", "medium", "ffmpeg encoding preset")
	flags.BoolVar(&opts.SkipTelemetry, "skip-telemetry", false, "Skip telemetry extraction")
	flags.BoolVar(&opts.SkipGPX, "skip-gpx", false, "Skip GPX track")
	flags.BoolVar(&opts.SkipCharts, "skip-charts", false, "Skip standalone PNG charts")
	flags.BoolVar(&opts.SkipMarkdown, "skip-markdown", false, "Skip Markdown report")
	flags.BoolVar(&opts.SkipMetadata, "skip-metadata", false, "Skip metadata JSON")
	flags.BoolVar(&opts.SkipPDF, "skip-pdf", false, "Skip PDF briefing")
	flags.BoolVar(&opts.ZipOutput, "zip", false, "Bundle artifacts in a ZIP package")
	flags.IntVar(&opts.StartOffsetMS, "start-offset", 0, "Trim milliseconds from the start")
	flags.IntVar(&opts.EndTrimMS, "end-trim", 0, "Trim milliseconds from the end")
	flags.BoolVar(&noTUI, "no-tui", false, "Use plain command-line output")
	flags.BoolVarP(&verbose, "verbose", "v", false, "Log debug output")
	flags.BoolVarP(&recursive, "recursive", "r", false, "Search subdirectories in batch mode")
	process := &cobra.Command{Use: "process [file]", Short: "Process one source", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		input := ""
		if len(args) > 0 {
			input = args[0]
		}
		if !noTUI && term.IsTerminal(int(os.Stdin.Fd())) {
			return runInteractive(input)
		}
		if input == "" {
			return fmt.Errorf("downwash: a source file is required with --no-tui")
		}
		return runFile(cmd.Context(), input)
	}}
	batch := &cobra.Command{Use: "batch directory", Short: "Process MP4 videos in a directory", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error { return runBatch(cmd.Context(), args[0]) }}
	var listen string
	var public bool
	mcpCommand := &cobra.Command{Use: "mcp", Short: "Serve full local automation over MCP stdio, or uploaded telemetry over HTTP", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if listen == "" {
			return mcpserver.New(version, true).Run(cmd.Context(), &mcp.StdioTransport{})
		}
		handler, err := mcpserver.Handler(version, os.Getenv("DOWNWASH_MCP_TOKEN"), public)
		if err != nil {
			return err
		}
		server := &http.Server{Addr: listen, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 90 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second}
		go func() {
			<-cmd.Context().Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		}()
		return server.ListenAndServe()
	}}
	mcpCommand.Flags().StringVar(&listen, "listen", "", "Serve /mcp and /health over HTTP at an address such as 127.0.0.1:8080; deploy behind HTTPS")
	mcpCommand.Flags().BoolVar(&public, "public", false, "Explicitly allow unauthenticated upload-only HTTP tools; local filesystem/video tools remain unavailable")
	root.AddCommand(mcpCommand)
	root.AddCommand(process, batch, &cobra.Command{Use: "version", Short: "Print version", Args: cobra.NoArgs, Run: func(cmd *cobra.Command, args []string) { fmt.Fprintln(cmd.OutOrStdout(), version) }})
	return root
}
