package main

import (
	"log"
	"tensorvault/cmd/tv/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		log.Fatal(err)
	}
}
