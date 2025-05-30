package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/lightninglabs/lightning-terminal/accounts"
	"github.com/lightninglabs/lightning-terminal/litrpc"
	"github.com/lightningnetwork/lnd/lncfg"
	"github.com/urfave/cli"
)

const (
	idName    = "id"
	labelName = "label"
)

var accountsCommands = []cli.Command{
	{
		Name:      "accounts",
		ShortName: "a",
		Usage:     "Manage accounts",
		Category:  "Accounts",
		Subcommands: []cli.Command{
			createAccountCommand,
			updateAccountCommand,
			listAccountsCommand,
			accountInfoCommand,
			removeAccountCommand,
		},
		Description: "Manage accounts.",
	},
}

var createAccountCommand = cli.Command{
	Name:      "create",
	ShortName: "c",
	Usage:     "Create a new off-chain account with a balance.",
	ArgsUsage: "balance [expiration_date] [--label=LABEL] [--save_to=FILE]",
	Description: `Adds an entry to the account database.
This entry represents an amount of satoshis (account balance) that can be spent
using off-chain transactions (e.g. paying invoices).

Macaroons can be created to be locked to an account. This makes sure that the
bearer of the macaroon can only spend at most that amount of satoshis through
the daemon that has issued the macaroon.

Accounts only assert a maximum amount spendable. Having a certain account
balance does not guarantee that the node has the channel liquidity to actually
spend that amount.`,
	Flags: []cli.Flag{
		cli.Uint64Flag{
			Name:  "balance",
			Usage: "The initial balance of the account.",
		},
		cli.Int64Flag{
			Name: "expiration_date",
			Usage: "The expiration date of the account expressed " +
				"in seconds since the unix epoch. 0 means " +
				"it does not expire.",
		},
		cli.StringFlag{
			Name: "save_to",
			Usage: "Store the account macaroon created for the " +
				"account to the given file.",
		},
		cli.StringFlag{
			Name:  labelName,
			Usage: "(optional) The unique label of the account.",
		},
	},
	Action: createAccount,
}

func createAccount(cli *cli.Context) error {
	ctx := getContext()
	clientConn, cleanup, err := connectClient(cli, false)
	if err != nil {
		return err
	}
	defer cleanup()
	client := litrpc.NewAccountsClient(clientConn)

	var (
		initialBalance uint64
		expirationDate int64
	)
	args := cli.Args()

	switch {
	case cli.IsSet("balance"):
		initialBalance = cli.Uint64("balance")
	case args.Present():
		initialBalance, err = strconv.ParseUint(args.First(), 10, 64)
		if err != nil {
			return fmt.Errorf("unable to decode balance %v", err)
		}
		args = args.Tail()
	}

	switch {
	case cli.IsSet("expiration_date"):
		expirationDate = cli.Int64("expiration_date")
	case args.Present():
		expirationDate, err = strconv.ParseInt(args.First(), 10, 64)
		if err != nil {
			return fmt.Errorf(
				"unable to decode expiration_date: %v", err,
			)
		}
		args = args.Tail()
	}

	req := &litrpc.CreateAccountRequest{
		AccountBalance: initialBalance,
		ExpirationDate: expirationDate,
		Label:          cli.String(labelName),
	}
	resp, err := client.CreateAccount(ctx, req)
	if err != nil {
		return err
	}

	printRespJSON(resp)

	// User requested to store the newly baked account macaroon to a file
	// in addition to printing it to the console.
	if cli.IsSet("save_to") {
		fileName := lncfg.CleanAndExpandPath(cli.String("save_to"))
		err := os.WriteFile(fileName, resp.Macaroon, 0644)
		if err != nil {
			return fmt.Errorf("error writing account macaroon "+
				"to %s: %v", fileName, err)
		}

		fmt.Printf("Account macaroon saved to %s\n", fileName)
	}

	return nil
}

var updateAccountCommand = cli.Command{
	Name:      "update",
	ShortName: "u",
	Usage:     "Update an existing off-chain account.",
	ArgsUsage: "[id | label] new_balance [new_expiration_date] [--save_to=]",
	Description: "Updates an existing off-chain account and sets " +
		"either a new balance or new expiration date or both.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  idName,
			Usage: "The ID of the account to update.",
		},
		cli.StringFlag{
			Name:  labelName,
			Usage: "(optional) The unique label of the account.",
		},
		cli.Int64Flag{
			Name: "new_balance",
			Usage: "(deprecated) The new balance of the account; " +
				"-1 means do not update the balance.",
			Value:  -1,
			Hidden: true,
		},
		cli.Int64Flag{
			Name: "new_expiration_date",
			Usage: "The new expiration date of the account " +
				"expressed in seconds since the unix epoch; " +
				"-1 means do not update the expiration date; " +
				"0 means it does not expire.",
			Value: -1,
		},
	},
	Action: updateAccount,
	Subcommands: []cli.Command{
		creditCommand,
		debitCommand,
	},
}

func updateAccount(cli *cli.Context) error {
	ctx := getContext()
	clientConn, cleanup, err := connectClient(cli, false)
	if err != nil {
		return err
	}
	defer cleanup()
	client := litrpc.NewAccountsClient(clientConn)

	id, label, args, err := parseIDOrLabel(cli)
	if err != nil {
		return err
	}

	var (
		newBalance     int64
		expirationDate int64
	)
	switch {
	case cli.IsSet("new_balance"):
		newBalance = cli.Int64("new_balance")
	case args.Present():
		newBalance, err = strconv.ParseInt(args.First(), 10, 64)
		if err != nil {
			return fmt.Errorf("unable to decode balance %v", err)
		}
		args = args.Tail()
	}

	switch {
	case cli.IsSet("new_expiration_date"):
		expirationDate = cli.Int64("new_expiration_date")
	case args.Present():
		expirationDate, err = strconv.ParseInt(args.First(), 10, 64)
		if err != nil {
			return fmt.Errorf(
				"unable to decode expiration_date: %v", err,
			)
		}
		args = args.Tail()
	}

	req := &litrpc.UpdateAccountRequest{
		Id:             id,
		Label:          label,
		AccountBalance: newBalance,
		ExpirationDate: expirationDate,
	}
	resp, err := client.UpdateAccount(ctx, req)
	if err != nil {
		return err
	}

	printRespJSON(resp)
	return nil
}

var creditCommand = cli.Command{
	Name:      "credit",
	ShortName: "c",
	Usage:     "Increase an account's balance by the given amount.",
	ArgsUsage: "[id | label] amount",
	Description: "Increases an existing off-chain account's balance by " +
		"the given amount.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  idName,
			Usage: "The ID of the account to credit.",
		},
		cli.StringFlag{
			Name:  labelName,
			Usage: "(optional) The unique label of the account.",
		},
		cli.Uint64Flag{
			Name:  "amount",
			Usage: "The amount to credit the account.",
		},
	},
	Action: creditBalance,
}

func creditBalance(cli *cli.Context) error {
	return updateBalance(cli, true)
}

var debitCommand = cli.Command{
	Name:      "debit",
	ShortName: "d",
	Usage:     "Decrease an account's balance by the given amount.",
	ArgsUsage: "[id | label] amount",
	Description: "Decreases an existing off-chain account's balance by " +
		"the given amount.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  idName,
			Usage: "The ID of the account to debit.",
		},
		cli.StringFlag{
			Name:  labelName,
			Usage: "(optional) The unique label of the account.",
		},
		cli.Uint64Flag{
			Name:  "amount",
			Usage: "The amount to debit the account.",
		},
	},
	Action: debitBalance,
}

func debitBalance(cli *cli.Context) error {
	return updateBalance(cli, false)
}

func updateBalance(cli *cli.Context, add bool) error {
	ctx := getContext()
	clientConn, cleanup, err := connectClient(cli, false)
	if err != nil {
		return err
	}
	defer cleanup()
	client := litrpc.NewAccountsClient(clientConn)

	account, args, err := parseAccountIdentifier(cli)
	if err != nil {
		return err
	}

	if (!cli.IsSet("amount") && len(args) != 1) ||
		(cli.IsSet("amount") && len(args) != 0) {

		return errors.New("invalid number of arguments")
	}

	var amount uint64
	switch {
	case cli.IsSet("amount"):
		amount = cli.Uint64("amount")
	case args.Present():
		amount, err = strconv.ParseUint(args.First(), 10, 64)
		if err != nil {
			return fmt.Errorf("unable to decode amount %v", err)
		}
		args = args.Tail()
	default:
		return errors.New("must set a value for amount")
	}

	if add {
		req := &litrpc.CreditAccountRequest{
			Account: account,
			Amount:  amount,
		}

		resp, err := client.CreditAccount(ctx, req)
		if err != nil {
			return err
		}

		printRespJSON(resp)
	} else {
		req := &litrpc.DebitAccountRequest{
			Account: account,
			Amount:  amount,
		}

		resp, err := client.DebitAccount(ctx, req)
		if err != nil {
			return err
		}

		printRespJSON(resp)
	}

	return nil
}

var listAccountsCommand = cli.Command{
	Name:      "list",
	ShortName: "l",
	Usage:     "List all off-chain accounts.",
	Description: "Returns all accounts that are currently stored in " +
		"the account database.",
	Action: listAccounts,
}

func listAccounts(cli *cli.Context) error {
	ctx := getContext()
	clientConn, cleanup, err := connectClient(cli, false)
	if err != nil {
		return err
	}
	defer cleanup()
	client := litrpc.NewAccountsClient(clientConn)

	req := &litrpc.ListAccountsRequest{}
	resp, err := client.ListAccounts(ctx, req)
	if err != nil {
		return err
	}

	printRespJSON(resp)
	return nil
}

var accountInfoCommand = cli.Command{
	Name:      "info",
	ShortName: "i",
	Usage:     "Show information about a single off-chain account.",
	ArgsUsage: "[id | label]",
	Description: "Returns a single account entry from the account " +
		"database.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  idName,
			Usage: "The ID of the account.",
		},
		cli.StringFlag{
			Name:  labelName,
			Usage: "(optional) The unique label of the account.",
		},
	},
	Action: accountInfo,
}

func accountInfo(cli *cli.Context) error {
	ctx := getContext()
	clientConn, cleanup, err := connectClient(cli, false)
	if err != nil {
		return err
	}
	defer cleanup()
	client := litrpc.NewAccountsClient(clientConn)

	id, label, _, err := parseIDOrLabel(cli)
	if err != nil {
		return err
	}

	req := &litrpc.AccountInfoRequest{
		Id:    id,
		Label: label,
	}
	resp, err := client.AccountInfo(ctx, req)
	if err != nil {
		return err
	}

	printRespJSON(resp)
	return nil
}

var removeAccountCommand = cli.Command{
	Name:        "remove",
	ShortName:   "r",
	Usage:       "Remove an off-chain account from the database.",
	ArgsUsage:   "[id | label]",
	Description: "Removes an account entry from the account database.",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  idName,
			Usage: "The ID of the account.",
		},
		cli.StringFlag{
			Name:  labelName,
			Usage: "(optional) The unique label of the account.",
		},
	},
	Action: removeAccount,
}

func removeAccount(cli *cli.Context) error {
	ctx := getContext()
	clientConn, cleanup, err := connectClient(cli, false)
	if err != nil {
		return err
	}
	defer cleanup()
	client := litrpc.NewAccountsClient(clientConn)

	id, label, _, err := parseIDOrLabel(cli)
	if err != nil {
		return err
	}

	req := &litrpc.RemoveAccountRequest{
		Id:    id,
		Label: label,
	}
	_, err = client.RemoveAccount(ctx, req)
	return err
}

// parseAccountIdentifier parses either the id or label from the command line,
// and returns the account identifier.
func parseAccountIdentifier(ctx *cli.Context) (
	*litrpc.AccountIdentifier, cli.Args, error) {

	id, label, args, err := parseIDOrLabel(ctx)
	if err != nil {
		return nil, nil, err
	}

	if id == "" && label == "" {
		return nil, nil, fmt.Errorf("id argument missing")
	}

	if id != "" {
		identifier := &litrpc.AccountIdentifier{
			Identifier: &litrpc.AccountIdentifier_Id{
				Id: id,
			},
		}

		return identifier, args, nil
	} else {
		identifier := &litrpc.AccountIdentifier{
			Identifier: &litrpc.AccountIdentifier_Label{
				Label: label,
			},
		}

		return identifier, args, nil
	}
}

// parseIDOrLabel parses either the id or label from the command line.
func parseIDOrLabel(ctx *cli.Context) (string, string, cli.Args, error) {
	var (
		accountID string
		label     string
	)
	args := ctx.Args()

	switch {
	case ctx.IsSet(idName) && ctx.IsSet(labelName):
		return "", "", nil, fmt.Errorf("either account ID or label " +
			"must be specified, not both")

	case ctx.IsSet(idName):
		accountID = ctx.String(idName)

	case ctx.IsSet(labelName):
		label = ctx.String(labelName)

	case args.Present():
		accountID = args.First()
		args = args.Tail()

		// Since we have a positional argument, we cannot be sure it's
		// an ID. So we check if it's an ID by trying to hex decode it
		// and by checking the length. This will break if the user
		// chooses labels that are also valid hex encoded IDs. But since
		// the label is supposed to be human-readable, this should be
		// unlikely.
		_, err := hex.DecodeString(accountID)
		if len(accountID) != hex.EncodedLen(accounts.AccountIDLen) ||
			err != nil {

			label = accountID
			accountID = ""
		}

	default:
		return "", "", nil, fmt.Errorf("id argument missing")
	}

	return accountID, label, args, nil
}
