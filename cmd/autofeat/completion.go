package main

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/painlesshippo/autofeat/internal/templates"
	"github.com/spf13/cobra"
)

func completeFeatureSelectors(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return featureCompletions(args, toComplete)
}

func completeFeatureName(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return featureCompletions(nil, toComplete)
}

func featureCompletions(selected []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	sessions, err := state.ListSessions()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	selectedNames := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		selectedNames[name] = struct{}{}
	}
	names := make([]string, 0, len(sessions))
	for name := range sessions {
		if _, exists := selectedNames[name]; exists || !strings.HasPrefix(name, toComplete) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

func completeTemplateNames(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names, err := templates.Names()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	matchingNames := names[:0]
	for _, name := range names {
		if strings.HasPrefix(name, toComplete) {
			matchingNames = append(matchingNames, name)
		}
	}
	return matchingNames, cobra.ShellCompDirectiveNoFileComp
}

func completeNothing(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func completeNewFeatureArguments(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return featureCompletions(nil, toComplete)
	case 1:
		return nil, cobra.ShellCompDirectiveFilterDirs
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func completeSessionRepositories(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return featureCompletions(nil, toComplete)
	}
	if len(args) > 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	session, err := state.GetSession(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	seen := make(map[string]struct{}, 2*len(session.Repos))
	values := make([]string, 0, 2*len(session.Repos))
	for _, repository := range session.Repos {
		for _, candidate := range []string{filepath.Base(repository.WorktreePath), repository.OriginalPath} {
			if _, exists := seen[candidate]; exists || candidate == "" || !strings.HasPrefix(candidate, toComplete) {
				continue
			}
			seen[candidate] = struct{}{}
			values = append(values, candidate)
		}
	}
	sort.Strings(values)
	return values, cobra.ShellCompDirectiveNoFileComp
}

func completeDirectories(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

func completeFilePaths(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveDefault
}

func registerFlagCompletion(command *cobra.Command, flagName string, completion cobra.CompletionFunc) {
	if err := command.RegisterFlagCompletionFunc(flagName, completion); err != nil {
		panic(err)
	}
}
