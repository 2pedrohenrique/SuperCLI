package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/2pedrohenrique/SuperCLI/internal/action"
	"github.com/2pedrohenrique/SuperCLI/internal/bootstrap"
	"github.com/2pedrohenrique/SuperCLI/internal/capture"
	"github.com/2pedrohenrique/SuperCLI/internal/config"
	"github.com/2pedrohenrique/SuperCLI/internal/kernel"
	"github.com/2pedrohenrique/SuperCLI/internal/notify"
	"github.com/2pedrohenrique/SuperCLI/internal/pluginhost"
	processes "github.com/2pedrohenrique/SuperCLI/internal/process"
	"github.com/2pedrohenrique/SuperCLI/internal/ui"
	"github.com/2pedrohenrique/SuperCLI/internal/workstation"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "supercli:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("supercli", flag.ContinueOnError)
	configPath := flags.String("config", defaultConfigPath(), "path to the YAML configuration")
	initConfig := flags.Bool("init", false, "write an example configuration and exit")
	showVersion := flags.Bool("version", false, "print version and exit")
	pluginDir := flags.String("plugin-dir", defaultPluginDir(), "directory containing community plugins")
	listPlugins := flags.Bool("list-plugins", false, "list discovered community plugins and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println("SuperCLI", version)
		return nil
	}
	if *initConfig {
		if err := bootstrap.WriteExample(*configPath); err != nil {
			return err
		}
		fmt.Println("Created", *configPath)
		return nil
	}
	plugins, err := pluginhost.Discover(*pluginDir)
	if err != nil {
		return err
	}
	if *listPlugins {
		if len(plugins) == 0 {
			fmt.Println("No community plugins found in", *pluginDir)
			return nil
		}
		for _, plugin := range plugins {
			fmt.Printf("%s\t%s\t%s\t%d actions\n", plugin.Manifest.ID, plugin.Manifest.Version, plugin.Manifest.Name, len(plugin.Manifest.Actions))
		}
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		if os.IsNotExist(errors.Unwrap(err)) {
			return fmt.Errorf("configuration not found; run supercli --init --config %q", *configPath)
		}
		return err
	}

	var notifier notify.Notifier = notify.Noop{}
	if cfg.Notifications.Enabled {
		notifier = notify.NewDesktop()
	}
	supervisor := processes.New(cfg, notifier)
	workstationPlugin, err := workstation.NewWithCommunityPlugins(cfg, *configPath, supervisor, capture.New(cfg.CaptureDir), action.NewRunner(), plugins)
	if err != nil {
		return fmt.Errorf("load community plugins: %w", err)
	}
	appKernel := kernel.New()
	if err := appKernel.Register(workstationPlugin); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := appKernel.Start(ctx); err != nil {
		return err
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = appKernel.Stop(shutdownCtx)
	}()

	program := tea.NewProgram(ui.New(ctx, workstationPlugin))
	if _, err := program.Run(); err != nil && !errors.Is(err, tea.ErrInterrupted) {
		return fmt.Errorf("run TUI: %w", err)
	}
	return nil
}

func defaultPluginDir() string {
	if value := os.Getenv("SUPERCLI_PLUGIN_DIR"); value != "" {
		return value
	}
	if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
		return filepath.Join(localAppData, "SuperCLI", "plugins")
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "supercli", "plugins")
	}
	return "plugins"
}

func defaultConfigPath() string {
	if value := os.Getenv("SUPERCLI_CONFIG"); value != "" {
		return value
	}
	if _, err := os.Stat("supercli.yaml"); err == nil {
		return "supercli.yaml"
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "supercli", "config.yaml")
	}
	return "supercli.yaml"
}
