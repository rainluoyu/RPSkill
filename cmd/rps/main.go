package main

import (
	"github.com/luoyu/rpskill/internal/cli"
	"os"
)

func main() { os.Exit(cli.Execute(cli.NewRoot())) }
