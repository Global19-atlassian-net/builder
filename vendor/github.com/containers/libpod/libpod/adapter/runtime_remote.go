// +build remoteclient

package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/containers/image/types"
	"github.com/containers/libpod/cmd/podman/varlink"
	"github.com/containers/libpod/libpod"
	"github.com/containers/libpod/libpod/image"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/varlink/go/varlink"
)

// ImageRuntime is wrapper for image runtime
type RemoteImageRuntime struct{}

// RemoteRuntime describes a wrapper runtime struct
type RemoteRuntime struct {
	Conn   *varlink.Connection
	Remote bool
}

// LocalRuntime describes a typical libpod runtime
type LocalRuntime struct {
	*RemoteRuntime
}

// GetRuntime returns a LocalRuntime struct with the actual runtime embedded in it
func GetRuntime(c *cli.Context) (*LocalRuntime, error) {
	runtime := RemoteRuntime{}
	conn, err := runtime.Connect()
	if err != nil {
		return nil, err
	}
	rr := RemoteRuntime{
		Conn:   conn,
		Remote: true,
	}
	foo := LocalRuntime{
		&rr,
	}
	return &foo, nil
}

// Shutdown is a bogus wrapper for compat with the libpod runtime
func (r RemoteRuntime) Shutdown(force bool) error {
	return nil
}

// ContainerImage
type ContainerImage struct {
	remoteImage
}

type remoteImage struct {
	ID          string
	Labels      map[string]string
	RepoTags    []string
	RepoDigests []string
	Parent      string
	Size        int64
	Created     time.Time
	InputName   string
	Names       []string
	Digest      digest.Digest
	isParent    bool
	Runtime     *LocalRuntime
}

// Container ...
type Container struct {
	remoteContainer
}

// remoteContainer ....
type remoteContainer struct {
	Runtime *LocalRuntime
	config  *libpod.ContainerConfig
	state   *libpod.ContainerState
}

// GetImages returns a slice of containerimages over a varlink connection
func (r *LocalRuntime) GetImages() ([]*ContainerImage, error) {
	var newImages []*ContainerImage
	images, err := iopodman.ListImages().Call(r.Conn)
	if err != nil {
		return nil, err
	}
	for _, i := range images {
		name := i.Id
		if len(i.RepoTags) > 1 {
			name = i.RepoTags[0]
		}
		newImage, err := imageInListToContainerImage(i, name, r)
		if err != nil {
			return nil, err
		}
		newImages = append(newImages, newImage)
	}
	return newImages, nil
}

func imageInListToContainerImage(i iopodman.ImageInList, name string, runtime *LocalRuntime) (*ContainerImage, error) {
	created, err := splitStringDate(i.Created)
	if err != nil {
		return nil, err
	}
	ri := remoteImage{
		InputName:   name,
		ID:          i.Id,
		Labels:      i.Labels,
		RepoTags:    i.RepoTags,
		RepoDigests: i.RepoTags,
		Parent:      i.ParentId,
		Size:        i.Size,
		Created:     created,
		Names:       i.RepoTags,
		isParent:    i.IsParent,
		Runtime:     runtime,
	}
	return &ContainerImage{ri}, nil
}

// NewImageFromLocal returns a container image representation of a image over varlink
func (r *LocalRuntime) NewImageFromLocal(name string) (*ContainerImage, error) {
	img, err := iopodman.GetImage().Call(r.Conn, name)
	if err != nil {
		return nil, err
	}
	return imageInListToContainerImage(img, name, r)

}

// LoadFromArchiveReference creates an image from a local archive
func (r *LocalRuntime) LoadFromArchiveReference(ctx context.Context, srcRef types.ImageReference, signaturePolicyPath string, writer io.Writer) ([]*ContainerImage, error) {
	// TODO We need to find a way to leak certDir, creds, and the tlsverify into this function, normally this would
	// come from cli options but we don't want want those in here either.
	imageID, err := iopodman.PullImage().Call(r.Conn, srcRef.DockerReference().String(), "", "", signaturePolicyPath, true)
	if err != nil {
		return nil, err
	}
	newImage, err := r.NewImageFromLocal(imageID)
	if err != nil {
		return nil, err
	}
	return []*ContainerImage{newImage}, nil
}

// New calls into local storage to look for an image in local storage or to pull it
func (r *LocalRuntime) New(ctx context.Context, name, signaturePolicyPath, authfile string, writer io.Writer, dockeroptions *image.DockerRegistryOptions, signingoptions image.SigningOptions, forcePull bool) (*ContainerImage, error) {
	// TODO Creds needs to be figured out here too, like above
	tlsBool := dockeroptions.DockerInsecureSkipTLSVerify
	// Remember SkipTlsVerify is the opposite of tlsverify
	// If tlsBook is true or undefined, we do not skip
	SkipTlsVerify := false
	if tlsBool == types.OptionalBoolFalse {
		SkipTlsVerify = true
	}
	imageID, err := iopodman.PullImage().Call(r.Conn, name, dockeroptions.DockerCertPath, "", signaturePolicyPath, SkipTlsVerify)
	if err != nil {
		return nil, err
	}
	newImage, err := r.NewImageFromLocal(imageID)
	if err != nil {
		return nil, err
	}
	return newImage, nil
}

func splitStringDate(d string) (time.Time, error) {
	fields := strings.Fields(d)
	t := fmt.Sprintf("%sT%sZ", fields[0], fields[1])
	return time.ParseInLocation(time.RFC3339Nano, t, time.UTC)
}

// IsParent goes through the layers in the store and checks if i.TopLayer is
// the parent of any other layer in store. Double check that image with that
// layer exists as well.
func (ci *ContainerImage) IsParent() (bool, error) {
	return ci.remoteImage.isParent, nil
}

// ID returns the image ID as a string
func (ci *ContainerImage) ID() string {
	return ci.remoteImage.ID
}

// Names returns a string array of names associated with the image
func (ci *ContainerImage) Names() []string {
	return ci.remoteImage.Names
}

// Created returns the time the image was created
func (ci *ContainerImage) Created() time.Time {
	return ci.remoteImage.Created
}

// Size returns the size of the image
func (ci *ContainerImage) Size(ctx context.Context) (*uint64, error) {
	usize := uint64(ci.remoteImage.Size)
	return &usize, nil
}

// Digest returns the image's digest
func (ci *ContainerImage) Digest() digest.Digest {
	return ci.remoteImage.Digest
}

// Labels returns a map of the image's labels
func (ci *ContainerImage) Labels(ctx context.Context) (map[string]string, error) {
	return ci.remoteImage.Labels, nil
}

// Dangling returns a bool if the image is "dangling"
func (ci *ContainerImage) Dangling() bool {
	return len(ci.Names()) == 0
}

// TagImage ...
func (ci *ContainerImage) TagImage(tag string) error {
	_, err := iopodman.TagImage().Call(ci.Runtime.Conn, ci.ID(), tag)
	return err
}

// RemoveImage calls varlink to remove an image
func (r *LocalRuntime) RemoveImage(ctx context.Context, img *ContainerImage, force bool) (string, error) {
	return iopodman.RemoveImage().Call(r.Conn, img.InputName, force)
}

// History returns the history of an image and its layers
func (ci *ContainerImage) History(ctx context.Context) ([]*image.History, error) {
	var imageHistories []*image.History

	reply, err := iopodman.HistoryImage().Call(ci.Runtime.Conn, ci.InputName)
	if err != nil {
		return nil, err
	}
	for _, h := range reply {
		created, err := splitStringDate(h.Created)
		if err != nil {
			return nil, err
		}
		ih := image.History{
			ID:        h.Id,
			Created:   &created,
			CreatedBy: h.CreatedBy,
			Size:      h.Size,
			Comment:   h.Comment,
		}
		imageHistories = append(imageHistories, &ih)
	}
	return imageHistories, nil
}

// LookupContainer gets basic information about container over a varlink
// connection and then translates it to a *Container
func (r *LocalRuntime) LookupContainer(idOrName string) (*Container, error) {
	state, err := r.ContainerState(idOrName)
	if err != nil {
		return nil, err
	}
	config := r.Config(idOrName)
	if err != nil {
		return nil, err
	}

	rc := remoteContainer{
		r,
		config,
		state,
	}

	c := Container{
		rc,
	}
	return &c, nil
}

func (r *LocalRuntime) GetLatestContainer() (*Container, error) {
	return nil, libpod.ErrNotImplemented
}

// ContainerState returns the "state" of the container.
func (r *LocalRuntime) ContainerState(name string) (*libpod.ContainerState, error) { //no-lint
	reply, err := iopodman.ContainerStateData().Call(r.Conn, name)
	if err != nil {
		return nil, err
	}
	data := libpod.ContainerState{}
	if err := json.Unmarshal([]byte(reply), &data); err != nil {
		return nil, err
	}
	return &data, err

}

// Config returns a container config
func (r *LocalRuntime) Config(name string) *libpod.ContainerConfig {
	// TODO the Spec being returned is not populated.  Matt and I could not figure out why.  Will defer
	// further looking into it for after devconf.
	// The libpod function for this has no errors so we are kind of in a tough
	// spot here.  Logging the errors for now.
	reply, err := iopodman.ContainerConfig().Call(r.Conn, name)
	if err != nil {
		logrus.Error("call to container.config failed")
	}
	data := libpod.ContainerConfig{}
	if err := json.Unmarshal([]byte(reply), &data); err != nil {
		logrus.Error("failed to unmarshal container inspect data")
	}
	return &data

}
