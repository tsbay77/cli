package link

import (
	"fmt"
	"github.com/MakeNowJust/heredoc"
	"github.com/cli/cli/v2/api"
	"github.com/cli/cli/v2/internal/ghrepo"
	"github.com/cli/cli/v2/pkg/cmd/project/shared/client"
	"github.com/cli/cli/v2/pkg/cmd/project/shared/queries"
	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/cli/cli/v2/pkg/iostreams"
	"github.com/shurcooL/githubv4"
	"github.com/spf13/cobra"
	"net/http"
	"strconv"
)

type linkOpts struct {
	number    int32
	owner     string
	repo      string
	team      string
	projectID string
	repoID    string
	teamID    string
	format    string
	exporter  cmdutil.Exporter
}

type linkConfig struct {
	httpClient func() (*http.Client, error)
	client     *queries.Client
	opts       linkOpts
	io         *iostreams.IOStreams
}

type linkProjectToRepoMutation struct {
	LinkProjectV2ToRepository struct {
		Repository queries.Repository `graphql:"repository"`
	} `graphql:"linkProjectV2ToRepository(input:$input)"`
}

type linkProjectToTeamMutation struct {
	LinkProjectV2ToTeam struct {
		Team queries.Team `graphql:"team"`
	} `graphql:"linkProjectV2ToTeam(input:$input)"`
}

func NewCmdLink(f *cmdutil.Factory, runF func(config linkConfig) error) *cobra.Command {
	opts := linkOpts{}
	linkCmd := &cobra.Command{
		Short: "Link a project to a repository or a team",
		Use:   "link [<number>] [flag]",
		Example: heredoc.Doc(`
			# link monalisa's project 1 to her repository "my_repo"
			gh project link 1 --owner monalisa --repo my_repo

			# link monalisa's organization's project 1 to her team "my_team"
			gh project link 1 --owner my_organization --team my_team
		`),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := client.New(f)
			if err != nil {
				return err
			}

			if len(args) == 1 {
				num, err := strconv.ParseInt(args[0], 10, 32)
				if err != nil {
					return cmdutil.FlagErrorf("invalid number: %v", args[0])
				}
				opts.number = int32(num)
			}

			config := linkConfig{
				httpClient: f.HttpClient,
				client:     client,
				opts:       opts,
				io:         f.IOStreams,
			}

			if config.opts.repo != "" && config.opts.team != "" {
				return fmt.Errorf("specify only one of `--repo` or `--team`")
			} else if config.opts.repo == "" && config.opts.team == "" {
				return fmt.Errorf("specify either `--repo` or `--team`")
			}

			// allow testing of the command without actually running it
			if runF != nil {
				return runF(config)
			}
			return runLink(config)
		},
	}

	linkCmd.Flags().StringVar(&opts.owner, "owner", "", "Login of the owner. Use \"@me\" for the current user.")
	linkCmd.Flags().StringVarP(&opts.repo, "repo", "R", "", "The repository to be linked to this project")
	linkCmd.Flags().StringVarP(&opts.team, "team", "T", "", "The team to be linked to this project")
	cmdutil.AddFormatFlags(linkCmd, &opts.exporter)

	return linkCmd
}

func runLink(config linkConfig) error {
	canPrompt := config.io.CanPrompt()
	owner, err := config.client.NewOwner(canPrompt, config.opts.owner)
	if err != nil {
		return err
	}

	project, err := config.client.NewProject(canPrompt, owner, config.opts.number, false)
	if err != nil {
		return err
	}
	config.opts.projectID = project.ID

	httpClient, err := config.httpClient()
	if err != nil {
		return err
	}
	c := api.NewClientFromHTTP(httpClient)

	if config.opts.repo != "" {
		return linkRepo(c, owner, config)
	} else if config.opts.team != "" {
		return linkTeam(c, owner, config)
	}
	return nil
}

func linkRepo(c *api.Client, owner *queries.Owner, config linkConfig) error {
	repo, err := api.GitHubRepo(c, ghrepo.New(owner.Login, config.opts.repo))
	if err != nil {
		return err
	}
	config.opts.repoID = repo.ID

	query, variable := linkRepoArgs(config)
	err = config.client.Mutate("LinkProjectV2ToRepository", query, variable)
	if err != nil {
		return err
	}

	if config.opts.exporter != nil {
		return config.opts.exporter.Write(config.io, query.LinkProjectV2ToRepository.Repository)
	}
	return printResults(config, query.LinkProjectV2ToRepository.Repository.URL)
}

func linkTeam(c *api.Client, owner *queries.Owner, config linkConfig) error {
	team, err := api.OrganizationTeam(c, ghrepo.New(owner.Login, ""), config.opts.team)
	if err != nil {
		return err
	}
	config.opts.teamID = team.ID

	query, variable := linkTeamArgs(config)
	err = config.client.Mutate("LinkProjectV2ToTeam", query, variable)
	if err != nil {
		return err
	}

	if config.opts.exporter != nil {
		return config.opts.exporter.Write(config.io, query.LinkProjectV2ToTeam.Team)
	}
	return printResults(config, query.LinkProjectV2ToTeam.Team.URL)
}

func linkRepoArgs(config linkConfig) (*linkProjectToRepoMutation, map[string]interface{}) {
	return &linkProjectToRepoMutation{}, map[string]interface{}{
		"input": githubv4.LinkProjectV2ToRepositoryInput{
			ProjectID:    githubv4.ID(config.opts.projectID),
			RepositoryID: githubv4.ID(config.opts.repoID),
		},
	}
}

func linkTeamArgs(config linkConfig) (*linkProjectToTeamMutation, map[string]interface{}) {
	return &linkProjectToTeamMutation{}, map[string]interface{}{
		"input": githubv4.LinkProjectV2ToTeamInput{
			ProjectID: githubv4.ID(config.opts.projectID),
			TeamID:    githubv4.ID(config.opts.teamID),
		},
	}
}

func printResults(config linkConfig, url string) error {
	if !config.io.IsStdoutTTY() {
		return nil
	}

	_, err := fmt.Fprintf(config.io.Out, "%s\n", url)
	return err
}