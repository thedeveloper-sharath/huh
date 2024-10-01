package main

import (
	"fmt"

	"github.com/thedeveloper-sharath/huh/spinner"
)

func main() {
	_ = spinner.New().Title("Loading").Accessible(true).Run()
	fmt.Println("Done!")
}
