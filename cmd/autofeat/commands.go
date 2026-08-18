package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRootCommand() *cobra.Command {
	rootCommand := &cobra.Command{
		Use:           "autofeat",
		Short:         "Manage ephemeral Git worktrees for AI agent feature sessions",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(*cobra.Command, []string) error {
			return usageError()
		},
	}
	rootCommand.CompletionOptions.DisableDefaultCmd = true
	rootCommand.AddCommand(
		newFeatureCommand(),
		newOpenCommand(),
		newRunCommand(),
		newSyncCommand(),
		newStatusCommand(),
		newTeardownCommand(),
		newListCommand(),
		newTemplateCommand(),
		newConfigCommand(),
		newVersionCommand(),
		newCompletionCommand(rootCommand),
	)
	return rootCommand
}

func newFeatureCommand() *cobra.Command {
	var localPath string
	var remoteURL string
	var templateName string
	var baseBranch string
	command := &cobra.Command{
		Use:  "new FEATURE",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			featureName := args[0]
			if remoteURL != "" && !isRemoteURL(remoteURL) {
				return usageError()
			}
			if err := validateFeatureName(featureName); err != nil {
				return err
			}
			if templateName != "" {
				return instantiateTemplate(featureName, templateName)
			}
			if localPath != "" {
				return addLocalRepositoryWithRef(featureName, localPath, baseBranch)
			}
			if remoteURL != "" {
				return addRemoteRepositoryWithRef(featureName, remoteURL, baseBranch)
			}
			return addRepositoryWithRef(featureName, baseBranch)
		},
	}
	command.Flags().StringVar(&localPath, "local", "", "local repository path")
	command.Flags().StringVar(&remoteURL, "remote", "", "remote repository URL")
	command.Flags().StringVar(&templateName, "template", "", "create the feature from a template")
	command.Flags().StringVar(&baseBranch, "ref", "", "base Git reference")
	command.MarkFlagsMutuallyExclusive("local", "remote", "template")
	command.MarkFlagsMutuallyExclusive("template", "ref")
	command.ValidArgsFunction = completeNothing
	registerFlagCompletion(command, "local", completeDirectories)
	registerFlagCompletion(command, "remote", completeNothing)
	registerFlagCompletion(command, "template", completeTemplateNames)
	registerFlagCompletion(command, "ref", completeNothing)
	return command
}

func newOpenCommand() *cobra.Command {
	var copilot bool
	command := &cobra.Command{
		Use:  "open SELECTOR...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			handler := openFeatureCommand
			if copilot {
				handler = openCopilotCommand
			}
			return runSelectedFeatures(args, handler)
		},
	}
	command.Flags().BoolVar(&copilot, "copilot", false, "open the feature with Copilot CLI")
	command.ValidArgsFunction = completeFeatureSelectors
	return command
}

func newRunCommand() *cobra.Command {
	var task string
	command := &cobra.Command{
		Use:  "run SELECTOR...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSelectedFeatures(args, func(featureName string) error {
				return runFeatureCommand(featureName, task)
			})
		},
	}
	command.Flags().StringVar(&task, "task", "", "append an objective to TASK.md")
	command.ValidArgsFunction = completeFeatureSelectors
	registerFlagCompletion(command, "task", completeNothing)
	return command
}

func newSyncCommand() *cobra.Command {
	command := &cobra.Command{
		Use:  "sync SELECTOR...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSelectedFeatures(args, syncFeatureCommand)
		},
	}
	command.ValidArgsFunction = completeFeatureSelectors
	return command
}

func newStatusCommand() *cobra.Command {
	command := &cobra.Command{
		Use:  "status [SELECTOR...]",
		Args: cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			selectors := args
			if len(selectors) == 0 || shellExpandedWildcard(selectors) {
				selectors = []string{"*"}
			}
			return statusCommand(selectors)
		},
	}
	command.ValidArgsFunction = completeFeatureSelectors
	return command
}

func newTeardownCommand() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:  "teardown SELECTOR...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSelectedFeatures(args, func(featureName string) error {
				return teardownCommand(featureName, force)
			})
		},
	}
	command.Flags().BoolVar(&force, "force", false, "discard uncommitted changes")
	command.ValidArgsFunction = completeFeatureSelectors
	return command
}

func newListCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "list",
		Args:              cobra.NoArgs,
		ValidArgsFunction: completeNothing,
		RunE: func(command *cobra.Command, _ []string) error {
			return listSessionsTo(command.OutOrStdout())
		},
	}
}

func newTemplateCommand() *cobra.Command {
	command := &cobra.Command{
		Use: "template",
		RunE: func(*cobra.Command, []string) error {
			return usageError()
		},
	}
	command.AddCommand(
		&cobra.Command{
			Use:               "list",
			Args:              cobra.NoArgs,
			ValidArgsFunction: completeNothing,
			RunE: func(command *cobra.Command, _ []string) error {
				return listTemplatesTo(command.OutOrStdout())
			},
		},
		newTemplateShowCommand(),
		newTemplateSaveCommand(),
	)
	return command
}

func newTemplateShowCommand() *cobra.Command {
	command := &cobra.Command{
		Use:  "show NAME",
		Args: cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return showTemplateTo(command.OutOrStdout(), args[0])
		},
	}
	command.ValidArgsFunction = completeTemplateNames
	return command
}

func newTemplateSaveCommand() *cobra.Command {
	var featureName string
	command := &cobra.Command{
		Use:  "save NAME",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return saveTemplateFromSession(args[0], featureName)
		},
	}
	command.Flags().StringVar(&featureName, "from", "", "source feature session")
	if err := command.MarkFlagRequired("from"); err != nil {
		panic(err)
	}
	command.ValidArgsFunction = completeNothing
	registerFlagCompletion(command, "from", completeFeatureName)
	return command
}

func newConfigCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "config",
		Args:              cobra.NoArgs,
		ValidArgsFunction: completeNothing,
		RunE: func(*cobra.Command, []string) error {
			return openConfigCommand()
		},
	}
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:               "version",
		Args:              cobra.NoArgs,
		ValidArgsFunction: completeNothing,
		RunE: func(command *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(command.OutOrStdout(), "autofeat %s\ncommit: %s\nbuilt: %s\ngo: %s\n", version, commit, buildDatetime, goVersion)
			return err
		},
	}
}

func newCompletionCommand(rootCommand *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use: "completion",
		RunE: func(*cobra.Command, []string) error {
			return usageError()
		},
	}
	command.AddCommand(
		&cobra.Command{
			Use:               "bash",
			Args:              cobra.NoArgs,
			ValidArgsFunction: completeNothing,
			RunE: func(command *cobra.Command, _ []string) error {
				return rootCommand.GenBashCompletionV2(command.OutOrStdout(), true)
			},
		},
		&cobra.Command{
			Use:               "powershell",
			Args:              cobra.NoArgs,
			ValidArgsFunction: completeNothing,
			RunE: func(command *cobra.Command, _ []string) error {
				return rootCommand.GenPowerShellCompletion(command.OutOrStdout())
			},
		},
	)
	return command
}
