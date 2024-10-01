package main

import "github.com/thedeveloper-sharath/huh"

func main() {
	f := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Dynamic Help"),
			huh.NewInput().Title("Dynamic Help"),
			huh.NewInput().Title("Dynamic Help"),
		),
	)
	f.Run()
}
