package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/painlesshippo/autofeat/internal/artifact"
	"github.com/painlesshippo/autofeat/internal/state"
	"github.com/spf13/cobra"
)

func newArtifactCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "artifact",
		Short: "Record external artifacts (builds, work items, pull requests, resources) on a feature session",
		RunE: func(*cobra.Command, []string) error {
			return usageError()
		},
	}
	command.AddCommand(newArtifactAddCommand(), newArtifactListCommand(), newArtifactRemoveCommand())
	return command
}

func newArtifactAddCommand() *cobra.Command {
	var typeName string
	var repository string
	command := &cobra.Command{
		Use:   "add FEATURE REF",
		Short: "Record an artifact; its type is inferred from REF unless --type is given",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return addArtifactTo(command.OutOrStdout(), args[0], args[1], artifact.Type(typeName), repository)
		},
	}
	command.Flags().StringVar(&typeName, "type", "", "artifact type (required for plain ids)")
	command.Flags().StringVar(&repository, "repo", "", "attached repository the artifact relates to")
	command.ValidArgsFunction = completeFeatureThenNothing
	registerFlagCompletion(command, "type", completeArtifactTypes)
	registerFlagCompletion(command, "repo", completeArtifactRepositories)
	return command
}

func newArtifactListCommand() *cobra.Command {
	var asJSON bool
	command := &cobra.Command{
		Use:   "list FEATURE",
		Short: "List the artifacts recorded on a feature session",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			return listArtifactsTo(command.OutOrStdout(), args[0], asJSON)
		},
	}
	command.Flags().BoolVar(&asJSON, "json", false, "print artifacts as JSON")
	command.ValidArgsFunction = completeFeatureThenNothing
	return command
}

func newArtifactRemoveCommand() *cobra.Command {
	var typeName string
	var repository string
	command := &cobra.Command{
		Use:   "remove FEATURE REF",
		Short: "Forget a recorded artifact by its reference or id",
		Args:  cobra.ExactArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			return removeArtifactTo(command.OutOrStdout(), args[0], args[1], artifact.Type(typeName), repository)
		},
	}
	command.Flags().StringVar(&typeName, "type", "", "only match artifacts of this type")
	command.Flags().StringVar(&repository, "repo", "", "only match artifacts linked to this attached repository")
	command.ValidArgsFunction = completeArtifactReferences
	registerFlagCompletion(command, "type", completeArtifactTypes)
	registerFlagCompletion(command, "repo", completeArtifactRepositories)
	return command
}

func addArtifactTo(output io.Writer, featureName, reference string, artifactType artifact.Type, repository string) error {
	var recorded artifact.Artifact
	duplicate := false
	err := state.UpdateSession(featureName, func(session *state.Session) error {
		var err error
		recorded, err = artifact.Parse(reference, artifactType)
		if err != nil {
			return err
		}
		if repository != "" {
			index, err := findRepositoryByName(featureName, *session, repository)
			if err != nil {
				return err
			}
			recorded.Repo = filepath.Base(session.Repos[index].WorktreePath)
		}

		for _, existing := range session.Artifacts {
			if existing.Key() == recorded.Key() {
				recorded = existing
				duplicate = true
				return nil
			}
		}

		recorded.CreatedAt = time.Now().UTC()
		session.Artifacts = append(session.Artifacts, recorded)
		return nil
	})
	if err != nil {
		return err
	}
	if duplicate {
		_, err = fmt.Fprintf(output, "Artifact %s %s is already recorded on feature %s\n", recorded.Type, artifactLabel(recorded), featureName)
		return err
	}
	_, err = fmt.Fprintf(output, "Recorded %s %s on feature %s\n", recorded.Type, artifactLabel(recorded), featureName)
	return err
}

func listArtifactsTo(output io.Writer, featureName string, asJSON bool) error {
	session, err := state.GetSession(featureName)
	if err != nil {
		return err
	}
	if asJSON {
		artifacts := session.Artifacts
		if artifacts == nil {
			artifacts = []artifact.Artifact{}
		}
		contents, err := json.MarshalIndent(artifacts, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s\n", contents)
		return err
	}
	return writeArtifactTable(output, "", session.Artifacts)
}

func removeArtifactTo(output io.Writer, featureName, reference string, artifactType artifact.Type, repository string) error {
	var removed artifact.Artifact
	err := state.UpdateSession(featureName, func(session *state.Session) error {
		if artifactType != "" {
			if _, err := artifact.ParseType(string(artifactType)); err != nil {
				return err
			}
		}
		repositoryName := repository
		if repositoryName != "" {
			repositoryIndex, err := findRepositoryByName(featureName, *session, repositoryName)
			if err != nil {
				return err
			}
			repositoryName = filepath.Base(session.Repos[repositoryIndex].WorktreePath)
		}
		index, err := findArtifact(featureName, session.Artifacts, reference, artifactType, repositoryName)
		if err != nil {
			return err
		}

		removed = session.Artifacts[index]
		session.Artifacts = append(session.Artifacts[:index], session.Artifacts[index+1:]...)
		return nil
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Removed %s %s from feature %s\n", removed.Type, artifactLabel(removed), featureName)
	return err
}

// findArtifact matches reference against the recorded value or inferred
// identity first, then against an extracted id such as a build number. Only
// artifacts of artifactType and linked to repository are considered when
// those are set. More than one match is an error.
func findArtifact(featureName string, artifacts []artifact.Artifact, reference string, artifactType artifact.Type, repository string) (int, error) {
	reference = strings.TrimSpace(reference)
	key := ""
	if parsed, err := artifact.Parse(reference, ""); err == nil {
		key = parsed.Key()
	}
	matching := func(match func(artifact.Artifact) bool) []int {
		indexes := make([]int, 0, 1)
		for index, recorded := range artifacts {
			if artifactType != "" && recorded.Type != artifactType {
				continue
			}
			if repository != "" && recorded.Repo != repository {
				continue
			}
			if match(recorded) {
				indexes = append(indexes, index)
			}
		}
		return indexes
	}

	matches := matching(func(recorded artifact.Artifact) bool {
		return recorded.Value == reference || (key != "" && recorded.Key() == key)
	})
	if len(matches) == 0 {
		matches = matching(func(recorded artifact.Artifact) bool {
			return recorded.ID != "" && recorded.ID == reference
		})
	}
	switch len(matches) {
	case 0:
		return -1, fmt.Errorf("artifact %q is not recorded on feature %q", reference, featureName)
	case 1:
		return matches[0], nil
	}
	candidates := make([]string, 0, len(matches))
	for _, index := range matches {
		candidate := string(artifacts[index].Type)
		if artifacts[index].Repo != "" {
			candidate += " (repo " + artifacts[index].Repo + ")"
		}
		candidates = append(candidates, candidate)
	}
	return -1, fmt.Errorf("artifact %q is ambiguous in feature %q (matches %s); narrow it with --type or --repo",
		reference, featureName, strings.Join(candidates, ", "))
}

// writeArtifactTable prints artifacts as a table, prefixed with a FEATURE
// column when featureName is set.
func writeArtifactTable(output io.Writer, featureName string, artifacts []artifact.Artifact) error {
	writer := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	prefix, header := "", ""
	if featureName != "" {
		prefix, header = featureName+"\t", "FEATURE\t"
	}
	fmt.Fprintf(writer, "%sTYPE\tID\tREPO\tCREATED\tVALUE\n", header)
	for _, recorded := range artifacts {
		fmt.Fprintf(writer, "%s%s\t%s\t%s\t%s\t%s\n",
			prefix,
			recorded.Type,
			valueOrDash(recorded.ID),
			valueOrDash(recorded.Repo),
			recorded.CreatedAt.Format(time.RFC3339),
			recorded.Value,
		)
	}
	return writer.Flush()
}

func artifactLabel(recorded artifact.Artifact) string {
	if recorded.ID != "" {
		return recorded.ID
	}
	return recorded.Value
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// dropRepositoryArtifactLinks clears the repo of artifacts tied to a
// repository directory that is being detached from the session.
func dropRepositoryArtifactLinks(artifacts []artifact.Artifact, directory string) {
	for index := range artifacts {
		if artifacts[index].Repo == directory {
			artifacts[index].Repo = ""
		}
	}
}

func printTeardownArtifacts(output io.Writer, featureName string, artifacts []artifact.Artifact) error {
	if len(artifacts) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(output, "Artifacts recorded on feature %s:\n", featureName); err != nil {
		return err
	}
	return writeArtifactTable(output, "", artifacts)
}

func completeFeatureThenNothing(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return featureCompletions(nil, toComplete)
}

func completeArtifactTypes(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	values := make([]string, 0, len(artifact.Types()))
	for _, artifactType := range artifact.Types() {
		if strings.HasPrefix(string(artifactType), toComplete) {
			values = append(values, string(artifactType))
		}
	}
	return values, cobra.ShellCompDirectiveNoFileComp
}

func completeArtifactRepositories(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	session, err := state.GetSession(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	values := make([]string, 0, len(session.Repos))
	for _, repository := range session.Repos {
		if name := filepath.Base(repository.WorktreePath); strings.HasPrefix(name, toComplete) {
			values = append(values, name)
		}
	}
	sort.Strings(values)
	return values, cobra.ShellCompDirectiveNoFileComp
}

func completeArtifactReferences(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return featureCompletions(nil, toComplete)
	case 1:
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	session, err := state.GetSession(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	values := make([]string, 0, len(session.Artifacts))
	for _, recorded := range session.Artifacts {
		if strings.HasPrefix(recorded.Value, toComplete) {
			values = append(values, recorded.Value)
		}
	}
	return values, cobra.ShellCompDirectiveNoFileComp
}
