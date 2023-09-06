package main

import (
	"fmt"
	"os"

	"github.com/project-machine/bootkit/pkg/stubby"
	cli "github.com/urfave/cli/v2"
)

var sbatBuiltIn = `sbat,1,SBAT Version,sbat,1,https://github.com/rhboot/shim/blob/main/SBAT.md
stubby.puzzleos,2,PuzzleOS,stubby,1,https://github.com/puzzleos/stubby
linux.puzzleos,1,PuzzleOS,linux,1,NOURL`

var stubbyCmd = cli.Command{
	Name: "stubby",
	Subcommands: []*cli.Command{
		&cli.Command{
			Name:      "smoosh",
			ArgsUsage: "output-uki.efi stubby.efi vmlinuz initrd",
			Action:    doStubbySmoosh,
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "Put modified vars in <output>",
					Value:   "",
				},
				&cli.StringFlag{
					Name:  "cmdline",
					Usage: "Embed the provided kernel command line",
					Value: "",
				},
			},
		},
	},
}

func doStubbySmoosh(ctx *cli.Context) error {
	var err error
	args := ctx.Args().Slice()
	if len(args) != 4 {
		return fmt.Errorf("Got %d args, require 4", len(args))
	}
	output := args[0]
	stubEfi := args[1]
	kernel := args[2]
	initrd := args[3]

	sbat := sbatBuiltIn
	if ctx.String("sbat") != "" {
		content, err := os.ReadFile(ctx.String("sbat"))
		if err != nil {
			return err
		}
		sbat = string(content)
	}

	err = stubby.Smoosh(stubEfi, output, ctx.String("cmdline"), sbat, kernel, initrd)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Wrote to %s\n", output)

	return nil
}
