package command

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/YottaBot-Platform/yottactl/internal/config"
	"github.com/spf13/cobra"
)

// newConfigCmd builds `yottactl config {get-contexts,use-context,
// set-context,current-context}` — kubectl-shaped context
// management. Plan #60 Track B B1.
func newConfigCmd(env *Env) *cobra.Command {
	cfgCmd := &cobra.Command{
		Use:     "config",
		Short:   "Manage yottactl contexts (~/.config/yotta/config.yaml)",
		GroupID: groupID("Core"),
	}

	getContexts := &cobra.Command{
		Use:   "get-contexts",
		Short: "List configured contexts",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			if len(cfg.Contexts) == 0 {
				fmt.Println("(no contexts configured — run `yottactl auth token ...`)")
				return nil
			}
			tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(tw, "CURRENT\tNAME\tSERVER\tACCOUNT")
			for _, c := range cfg.Contexts {
				marker := ""
				if c.Name == cfg.CurrentContext {
					marker = "*"
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", marker, c.Name, c.Server, c.AccountID)
			}
			return tw.Flush()
		},
	}

	useContext := &cobra.Command{
		Use:   "use-context <name>",
		Short: "Switch the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			if err := cfg.UseContext(args[0]); err != nil {
				return &ExitError{Code: 3, Msg: err.Error()}
			}
			if err := cfg.Save(); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			fmt.Printf("switched to context %q\n", args[0])
			return nil
		},
	}

	var server, accountID string
	setContext := &cobra.Command{
		Use:   "set-context <name>",
		Short: "Create or update a context's server/account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			c := config.Context{Name: args[0]}
			if existing := cfg.Find(c.Name); existing != nil {
				c = *existing
			}
			// Credentials belong to a server/account pair. Metadata-only edits
			// preserve them; a destination change must not carry them along.
			if (cmd.Flags().Changed("server") && server != c.Server) ||
				(cmd.Flags().Changed("account-id") && accountID != c.AccountID) {
				c.Token, c.Login = "", nil
			}
			if cmd.Flags().Changed("server") {
				c.Server = server
			}
			if cmd.Flags().Changed("account-id") {
				c.AccountID = accountID
			}
			cfg.SetContext(c) // also makes it current
			if err := cfg.Save(); err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			fmt.Printf("set context %q\n", args[0])
			return nil
		},
	}
	setContext.Flags().StringVar(&server, "server", "", "server URL")
	setContext.Flags().StringVar(&accountID, "account-id", "", "tenant account id")

	currentContext := &cobra.Command{
		Use:   "current-context",
		Short: "Print the current context name",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			if cfg.CurrentContext == "" {
				return &ExitError{Code: 3, Msg: "no current context"}
			}
			fmt.Println(cfg.CurrentContext)
			return nil
		},
	}

	cfgCmd.AddCommand(getContexts, useContext, setContext, currentContext)
	return cfgCmd
}
