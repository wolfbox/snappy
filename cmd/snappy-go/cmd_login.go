package main

import (
	"fmt"

	"code.google.com/p/go.crypto/ssh/terminal"

	"launchpad.net/snappy/snappy"
)

type cmdLogin struct {
	Positional struct {
		UserName string `positional-arg-name:"userid" description:"Username for the login"`
	} `positional-args:"yes" required:"yes"`
}

const shortLoginHelp = `Log into the store`

const longLoginHelp = `This command logs out into the store`

func init() {
	var cmdLoginData cmdLogin
	_, _ = parser.AddCommand("login",
		shortLoginHelp,
		longLoginHelp,
		&cmdLoginData)
}

func (x *cmdLogin) Execute(args []string) (err error) {
	const tokenName = "snappy login token"

	username := x.Positional.UserName
	fmt.Print("Password: ")
	password, err := terminal.ReadPassword(0)
	fmt.Print("\n")
	if err != nil {
		return err
	}
	// FIXME: implement 2factor auth
	otp := ""
	token, err := snappy.RequestStoreToken(username, string(password), tokenName, otp)
	if err != nil {
		return err
	}

	return snappy.WriteStoreToken(*token)
}
