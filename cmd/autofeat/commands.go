package main

import (
	"errors"
	"fmt"
	"strings"

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
		newRemoveCommand(),
		newOpenCommand(),
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
	var templateName string
	var baseBranch string
	var azureBuild string
	var azureOrg string
	var azureProject string
	var azureURL string
	command := &cobra.Command{
		Use:  "new FEATURE [SOURCE]",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(command *cobra.Command, args []string) error {
			featureName := args[0]
			source := ""
			if len(args) == 2 {
				source = args[1]
				if source == "" || command.Flags().Changed("template") || command.Flags().Changed("azure-build") {
					return usageError()
				}
			}
			if azureBuild == "" && (azureOrg != "" || azureProject != "" || azureURL != "") {
				return usageError()
			}
			if err := validateFeatureName(featureName); err != nil {
				return err
			}
			if templateName != "" {
				return instantiateTemplate(featureName, templateName)
			}
			if azureBuild != "" {
				return addAzureBuildWorkspace(featureName, azureBuild, azureOrg, azureProject, azureURL)
			}
			if source == "" {
				return addRepositoryWithRef(featureName, baseBranch)
			}
			if isRemoteURL(source) {
				return addRemoteRepositoryWithRef(featureName, source, baseBranch)
			}
			return addLocalRepositoryWithRef(featureName, source, baseBranch)
		},
	}
	command.Flags().StringVar(&templateName, "template", "", "create the feature from a template")
	command.Flags().StringVar(&baseBranch, "ref", "", "base Git reference")
	command.Flags().StringVar(&azureBuild, "azure-build", "", "Azure DevOps build id or build results URL")
	command.Flags().StringVar(&azureOrg, "azure-org", "", "Azure DevOps organization (with a numeric --azure-build)")
	command.Flags().StringVar(&azureProject, "azure-project", "", "Azure DevOps project (with a numeric --azure-build)")
	command.Flags().StringVar(&azureURL, "azure-url", "", "Azure DevOps instance base URL (default https://dev.azure.com)")
	command.MarkFlagsMutuallyExclusive("template", "azure-build")
	command.MarkFlagsMutuallyExclusive("template", "ref")
	command.MarkFlagsMutuallyExclusive("azure-build", "ref")
	command.ValidArgsFunction = completeNewFeatureArguments
	registerFlagCompletion(command, "template", completeTemplateNames)
	registerFlagCompletion(command, "ref", completeNothing)
	registerFlagCompletion(command, "azure-build", completeNothing)
	registerFlagCompletion(command, "azure-org", completeNothing)
	registerFlagCompletion(command, "azure-project", completeNothing)
	registerFlagCompletion(command, "azure-url", completeNothing)
	return command
}

func newRemoveCommand() *cobra.Command {
	var force bool
	command := &cobra.Command{
		Use:  "remove FEATURE REPO",
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			featureName, repository := args[0], args[1]
			if repository == "" {
				return usageError()
			}
			if isRemoteURL(repository) {
				return removeRepositoryCommand(featureName, normalizeRemoteRepositoryURL(repository), true, force)
			}
			if repositoryRoot, ok := attachedLocalRepositoryRoot(featureName, repository); ok {
				return removeRepositoryCommand(featureName, repositoryRoot, false, force)
			}
			return removeRepositoryByNameCommand(featureName, repository, force)
		},
	}
	command.Flags().BoolVar(&force, "force", false, "discard uncommitted changes")
	command.ValidArgsFunction = completeSessionRepositories
	return command
}

func newOpenCommand() *cobra.Command {
	var copilot bool
	var devcontainerConfig string
	command := &cobra.Command{
		Use:  "open SELECTOR...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			switch {
			case command.Flags().Changed("devcontainer"):
				if strings.TrimSpace(devcontainerConfig) == "" {
					return errors.New("--devcontainer requires a devcontainer.json path")
				}
				return runSelectedFeatures(args, func(featureName string) error {
					return openDevcontainerCommand(featureName, devcontainerConfig)
				})
			case copilot:
				return runSelectedFeatures(args, openCopilotCommand)
			default:
				return runSelectedFeatures(args, openFeatureCommand)
			}
		},
	}
	command.Flags().BoolVar(&copilot, "copilot", false, "open the feature with Copilot CLI")
	command.Flags().StringVar(&devcontainerConfig, "devcontainer", "", "open the feature in the dev container defined by this devcontainer.json path")
	command.MarkFlagsMutuallyExclusive("copilot", "devcontainer")
	command.ValidArgsFunction = completeFeatureSelectors
	registerFlagCompletion(command, "devcontainer", completeFilePaths)
	return command
}

func newSyncCommand() *cobra.Command {
	var cancel bool
	command := &cobra.Command{
		Use:  "sync SELECTOR...",
		Args: cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if cancel {
				return runAllSelectedFeatures(args, cancelSyncCommand)
			}
			return runSelectedFeatures(args, syncFeatureCommand)
		},
	}
	command.Flags().BoolVar(&cancel, "cancel", false, "abort in-progress rebases left by an interrupted synchronization")
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
