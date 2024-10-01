package main

import (
	"github.com/thedeveloper-sharath/huh"
)

func main() {
	var happy bool

	confirm := huh.NewConfirm().
		Title("Are you sure? ").
		Description("Please confirm. ").
		Affirmative("Yes!").
		Negative("No.").
		Value(&happy)

	huh.NewForm(huh.NewGroup(confirm)).Run()
}
