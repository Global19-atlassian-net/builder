package main

import (
	"fmt"
	"os"
	"time"

	"github.com/containers/libpod/cmd/podman/cliconfig"
	"github.com/containers/libpod/cmd/podman/libpodruntime"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	waitCommand cliconfig.WaitValues

	waitDescription = `
	podman wait

	Block until one or more containers stop and then print their exit codes
`
	_waitCommand = &cobra.Command{
		Use:   "wait",
		Short: "Block on one or more containers",
		Long:  waitDescription,
		RunE: func(cmd *cobra.Command, args []string) error {
			waitCommand.InputArgs = args
			waitCommand.GlobalFlags = MainGlobalOpts
			return waitCmd(&waitCommand)
		},
		Example: "CONTAINER-NAME [CONTAINER-NAME ...]",
	}
)

func init() {
	waitCommand.Command = _waitCommand
	waitCommand.SetUsageTemplate(UsageTemplate())
	flags := waitCommand.Flags()
	flags.UintVarP(&waitCommand.Interval, "interval", "i", 250, "Milliseconds to wait before polling for completion")
	flags.BoolVarP(&waitCommand.Latest, "latest", "l", false, "Act on the latest container podman is aware of")
}

func waitCmd(c *cliconfig.WaitValues) error {
	args := c.InputArgs
	if len(args) < 1 && !c.Latest {
		return errors.Errorf("you must provide at least one container name or id")
	}

	runtime, err := libpodruntime.GetRuntime(&c.PodmanCommand)
	if err != nil {
		return errors.Wrapf(err, "error creating libpod runtime")
	}
	defer runtime.Shutdown(false)

	if err != nil {
		return errors.Wrapf(err, "could not get config")
	}

	var lastError error
	if c.Latest {
		latestCtr, err := runtime.GetLatestContainer()
		if err != nil {
			return errors.Wrapf(err, "unable to wait on latest container")
		}
		args = append(args, latestCtr.ID())
	}

	for _, container := range args {
		ctr, err := runtime.LookupContainer(container)
		if err != nil {
			return errors.Wrapf(err, "unable to find container %s", container)
		}
		if c.Interval == 0 {
			return errors.Errorf("interval must be greater then 0")
		}
		returnCode, err := ctr.WaitWithInterval(time.Duration(c.Interval) * time.Millisecond)
		if err != nil {
			if lastError != nil {
				fmt.Fprintln(os.Stderr, lastError)
			}
			lastError = errors.Wrapf(err, "failed to wait for the container %v", container)
		} else {
			fmt.Println(returnCode)
		}
	}

	return lastError
}
