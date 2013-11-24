package main

import (
	"os"

	"github.com/codegangsta/cli"

	"github.com/vito/gordon"
	"github.com/vito/gordon/commands"
)

func main() {
	app := cli.NewApp()
	app.Name = "gordon"
	app.Usage = "manage warden containers"
	app.Flags = []cli.Flag{
		cli.StringFlag{"socket", "/tmp/warden.sock", "path to the warden command socket"},
	}

	ui := commands.BasicUI{
		Writer: os.Stdout,
	}

	app.Commands = []cli.Command{
		{
			Name:  "list",
			Usage: "list running containers",
			Action: func(c *cli.Context) {
				list := commands.NewList(client(c), ui)
				list.Run()
			},
		},
		{
			Name:  "create",
			Usage: "create a container",
			Action: func(c *cli.Context) {
				create := commands.NewCreate(client(c), ui)
				create.Run()
			},
		},
		{
			Name:  "destroy",
			Usage: "destroy a container",
			Flags: []cli.Flag{
				cli.StringFlag{"handle", "", "handle of the container to destroy"},
			},
			Action: func(c *cli.Context) {
				handle := c.String("handle")
				destroy := commands.NewDestroy(client(c), ui, handle)
				destroy.Run()
			},
		},
		{
			Name:  "spawn",
			Usage: "spawn a job in a container",
			Flags: []cli.Flag{
				cli.StringFlag{"handle", "", "handle of the container to destroy"},
				cli.StringFlag{"script", "", "script to run in the container"},
			},
			Action: func(c *cli.Context) {
				handle := c.String("handle")
				script := c.String("script")
				spawn := commands.NewSpawn(client(c), ui, handle, script)
				spawn.Run()
			},
		},
		{
			Name:  "link",
			Usage: "link to a running job in a container",
			Flags: []cli.Flag{
				cli.StringFlag{"handle", "", "handle of the container to destroy"},
				cli.StringFlag{"job", "", "job id to attach"},
			},
			Action: func(c *cli.Context) {
				handle := c.String("handle")
				jobId := c.Int("job")
				link := commands.NewLink(client(c), ui, handle, uint32(jobId))
				link.Run()
			},
		},
	}

	app.Run(os.Args)
}

func client(c *cli.Context) warden.Client {
	connectionInfo := &warden.ConnectionInfo{
		SocketPath: c.GlobalString("socket"),
	}
	client := warden.NewClient(connectionInfo)
	client.Connect()

	return client
}
