package main

import (
	"fmt"
	"github.com/containers/libpod/cmd/podman/libpodruntime"
	"github.com/containers/libpod/cmd/podman/shared"
	"github.com/containers/libpod/libpod"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

var (
	rmFlags = []cli.Flag{
		cli.BoolFlag{
			Name:  "all, a",
			Usage: "Remove all containers",
		},
		cli.BoolFlag{
			Name:  "force, f",
			Usage: "Force removal of a running container.  The default is false",
		},
		LatestFlag,
		cli.BoolFlag{
			Name:  "volumes, v",
			Usage: "Remove the volumes associated with the container (Not implemented yet)",
		},
	}
	rmDescription = fmt.Sprintf(`
Podman rm will remove one or more containers from the host.
The container name or ID can be used. This does not remove images.
Running containers will not be removed without the -f option.
`)
	rmCommand = cli.Command{
		Name:                   "rm",
		Usage:                  "Remove one or more containers",
		Description:            rmDescription,
		Flags:                  sortFlags(rmFlags),
		Action:                 rmCmd,
		ArgsUsage:              "",
		UseShortOptionHandling: true,
		OnUsageError:           usageErrorHandler,
	}
)

// saveCmd saves the image to either docker-archive or oci
func rmCmd(c *cli.Context) error {
	var (
		delContainers []*libpod.Container
		lastError     error
		deleteFuncs   []shared.ParallelWorkerInput
	)

	ctx := getContext()
	if err := validateFlags(c, rmFlags); err != nil {
		return err
	}
	runtime, err := libpodruntime.GetRuntime(c)
	if err != nil {
		return errors.Wrapf(err, "could not get runtime")
	}
	defer runtime.Shutdown(false)

	if err := checkAllAndLatest(c); err != nil {
		return err
	}

	delContainers, lastError = getAllOrLatestContainers(c, runtime, -1, "all")

	for _, container := range delContainers {
		con := container
		f := func() error {
			return runtime.RemoveContainer(ctx, con, c.Bool("force"))
		}

		deleteFuncs = append(deleteFuncs, shared.ParallelWorkerInput{
			ContainerID:  con.ID(),
			ParallelFunc: f,
		})
	}
	maxWorkers := shared.Parallelize("rm")
	if c.GlobalIsSet("max-workers") {
		maxWorkers = c.GlobalInt("max-workers")
	}
	logrus.Debugf("Setting maximum workers to %d", maxWorkers)

	deleteErrors := shared.ParallelExecuteWorkerPool(maxWorkers, deleteFuncs)
	for cid, result := range deleteErrors {
		if result != nil {
			fmt.Println(result.Error())
			lastError = result
			continue
		}
		fmt.Println(cid)
	}
	return lastError
}
