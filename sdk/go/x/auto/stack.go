package auto

import (
	"github.com/pkg/errors"
	"github.com/pulumi/pulumi/sdk/v2/go/common/workspace"
)

type Stack interface {
	// -- Lifecycle

	// Up creates or updates the resources in a stack
	// https://www.pulumi.com/docs/reference/cli/pulumi_up/
	Up() (UpResult, error)
	// Preview preforms a dry-run update to a stack, returning pending changes
	// https://www.pulumi.com/docs/reference/cli/pulumi_preview/
	Preview() (PreviewResult, error)
	// Refresh refreshes the resources in a stack
	// https://www.pulumi.com/docs/reference/cli/pulumi_refresh/
	Refresh() (RefreshResult, error)
	// Destroy deletes all resources in a stack
	// https://www.pulumi.com/docs/reference/cli/pulumi_destroy/
	Destroy() (DestroyResult, error)
	// Remove removes a stack and its configuration
	// https://www.pulumi.com/docs/reference/cli/pulumi_stack_rm/
	Remove() error

	// -- Status

	// Summary returns information about the last update on the stack
	Summary() (UpdateSummary, error)
	// Outputs returns the current plaintext and secret stack outputs
	Outputs() (map[string]interface{}, map[string]interface{}, error)
	// User returns the current identity associated with the ambient $PULUMI_ACCESS_TOKEN
	User() (string, error)

	// -- Config

	// SetConfig sets (upsert) the specified config values
	SetConfig(map[string]string) error
	// SetSecrets sets (upsert) the specified secret config values
	SetSecrets(map[string]string) error

	// -- marker method
	isStack()
}

// NewStack creates a stack for deployment and other operations.
// Will select an existing matching stack if available before creating new.
// Merges configuration with existing Pulumi.yaml, Pulumi.<stack>.yaml
// Sets config and secret values if provided.
func NewStack(ss StackSpec) (Stack, error) {
	err := ss.validate()
	if err != nil {
		return nil, err
	}

	if ss.Project.Remote != nil {
		p, err := setupRemote(ss.Project.Remote)
		ss.Project.SourcePath = p
		if err != nil {
			return nil, errors.Wrap(err, "unable to enlist and setup remote")
		}
	}

	s := &stack{
		Name:        ss.Name,
		ProjectName: ss.Project.Name,
		SourcePath:  ss.Project.SourcePath,
	}

	err = s.initOrSelectStack()
	if err != nil {
		return nil, errors.Wrap(err, "could not initialize or select stack")
	}

	err = ss.writeProject()
	if err != nil {
		return nil, err
	}

	err = ss.writeStack()
	if err != nil {
		return nil, err
	}
	var config map[string]string
	var secrets map[string]string
	if ss.Overrides != nil {
		config = ss.Overrides.Config
		secrets = ss.Overrides.Secrets
	}
	err = s.setConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "unable to set config")
	}

	err = s.setSecrets(secrets)
	if err != nil {
		return nil, errors.Wrap(err, "unable to set secrets")
	}

	return s, nil
}

// StackSpec is a description of a pulumi stack
type StackSpec struct {
	// Name of the the stack
	Name string
	// Project is a description of the project to execute
	Project ProjectSpec
	// Overrides is an optional set of values to overwrite in pulumi.<stack>.yaml
	Overrides *StackOverrides
}

// ProjectSpec is a description of a pulumi project and corresponding source code
type ProjectSpec struct {
	// Name of the project
	Name string

	// (Local program) source directory containing the pulumi program
	SourcePath string

	// (Remote program) information to clone and setup a git hosted pulumi program
	Remote *RemoteArgs

	// Optional set of values to upsert into existing pulumi.yaml
	Overrides *ProjectOverrides
}

type RemoteArgs struct {
	// Git URL used to fetch the repository.
	RepoURL string

	// Optional path relative to the repo root specifying location of the pulumi program
	ProjectPath *string
	// Optional branch to checkout
	Branch *string
	// Optional commit to checkout
	CommitHash *string
	// Optional function to execute after enlisting in the specified repo.
	Setup SetupFn
	// Optional directory to enlist repo, defaults to using ioutil.TempDir.
	WorkDir *string
}

// SetupFn is called with a PATH containing a pulumi program after enlistment
type SetupFn func(string) error

// ProjectOverrides is an optional set of values to be merged with
// the existing pulumi.yaml
type ProjectOverrides struct {
	// Replace controls merge behavior with existing Pulumi.yaml files
	Replace bool
	Project *workspace.Project
}

// StackOverrides is an optional set of values to be merged with
// the existing pulumi.<stackName>.yaml
type StackOverrides struct {
	// Replace controls merge behavior with existing stack.yaml files.
	Replace bool
	// Config is an optional config bag to `pulumi config set`
	Config map[string]string
	// Secrets is an optional config bag to `pulumi config set --secret`
	Secrets map[string]string
	// TODO we should use a limited struct that prevents setting config directly
	// We want users to explicitly handle config/secrets through above param
	// ProjectStack is the optional set of overrides
	ProjectStack *workspace.ProjectStack
}

func (ss *StackSpec) validate() error {
	if ss.Name == "" {
		return errors.New("missing stack name")
	}
	if ss.Project.Name == "" {
		return errors.New("missing project name")
	}
	if ss.Project.SourcePath == "" && ss.Project.Remote == nil {
		return errors.New("must specify one of `ProjectSpec.Source` or `ProjectSpec.Remote`")
	}
	if ss.Project.Remote != nil {
		if ss.Project.SourcePath != "" {
			return errors.New(
				"`ProjectSpec.Source` and `ProjectSpec.Remote` are mutually exclusive, but both were provided",
			)
		}
		if ss.Project.Remote.RepoURL == "" {
			return errors.New("Missing git source URL `Project.Remote.RepoURL`")
		}
	}
	return nil
}

type stack struct {
	Name        string
	ProjectName string
	SourcePath  string
}

func (s *stack) isStack() {}