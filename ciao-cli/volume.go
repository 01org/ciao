//
// Copyright (c) 2016 Intel Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rackspace/gophercloud"
	"github.com/rackspace/gophercloud/openstack"
	"github.com/rackspace/gophercloud/openstack/blockstorage/v2/volumes"
	"github.com/rackspace/gophercloud/pagination"
)

var volumeCommand = &command{
	SubCommands: map[string]subCommand{
		"add":    new(volumeAddCommand),
		"list":   new(volumeListCommand),
		"show":   new(volumeShowCommand),
		"update": new(volumeUpdateCommand),
		"delete": new(volumeDeleteCommand),
	},
}

type volumeAddCommand struct {
	Flag        flag.FlagSet
	size        int
	description string
	name        string
	source      string
}

func (cmd *volumeAddCommand) usage(...string) {
	fmt.Fprintf(os.Stderr, `usage: ciao-cli [options] volume add [flags]

Create a new block storage volume

The add flags are:

`)
	cmd.Flag.PrintDefaults()
	os.Exit(2)
}

func (cmd *volumeAddCommand) parseArgs(args []string) []string {
	cmd.Flag.StringVar(&cmd.name, "name", "", "Volume name")
	cmd.Flag.StringVar(&cmd.source, "source", "", "Volume ID to clone from")
	cmd.Flag.IntVar(&cmd.size, "size", 1, "Size of the volume in GB")
	cmd.Flag.StringVar(&cmd.description, "description", "", "Volume description")
	cmd.Flag.Usage = func() { cmd.usage() }
	cmd.Flag.Parse(args)
	return cmd.Flag.Args()
}

func (cmd *volumeAddCommand) run(args []string) error {
	client, err := storageServiceClient(*identityUser, *identityPassword, *tenantID)
	if err != nil {
		fatalf("Could not get volume service client [%s]\n", err)
	}

	opts := volumes.CreateOpts{
		Description: cmd.description,
		Name:        cmd.name,
		Size:        cmd.size,
		SourceVolID: cmd.source,
	}

	vol, err := volumes.Create(client, opts).Extract()
	if err == nil {
		fmt.Printf("Created new volume: %s\n", vol.ID)
	}
	return err
}

type volumeListCommand struct {
	Flag flag.FlagSet
}

func (cmd *volumeListCommand) usage(...string) {
	fmt.Fprintf(os.Stderr, `usage: ciao-cli [options] volume list

List all volumes
`)
	os.Exit(2)
}

func (cmd *volumeListCommand) parseArgs(args []string) []string {
	cmd.Flag.Usage = func() { cmd.usage() }
	cmd.Flag.Parse(args)
	return cmd.Flag.Args()
}

func (cmd *volumeListCommand) run(args []string) error {
	client, err := storageServiceClient(*identityUser, *identityPassword, *tenantID)
	if err != nil {
		fatalf("Could not get volume service client [%s]\n", err)
	}

	pager := volumes.List(client, volumes.ListOpts{})

	err = pager.EachPage(func(page pagination.Page) (bool, error) {
		volumeList, err := volumes.ExtractVolumes(page)
		if err != nil {
			errorf("Could not extract volume [%s]\n", err)
		}
		for i, v := range volumeList {
			fmt.Printf("Volume #%d\n", i+1)
			dumpVolume(&v)
			fmt.Printf("\n")
		}

		return false, nil
	})

	return err
}

type volumeShowCommand struct {
	Flag   flag.FlagSet
	volume string
}

func (cmd *volumeShowCommand) usage(...string) {
	fmt.Fprintf(os.Stderr, `usage: ciao-cli [options] volume show [flags]

Show information about a volume

The show flags are:
`)
	cmd.Flag.PrintDefaults()
	os.Exit(2)
}

func (cmd *volumeShowCommand) parseArgs(args []string) []string {
	cmd.Flag.StringVar(&cmd.volume, "volume", "", "Volume UUID")
	cmd.Flag.Usage = func() { cmd.usage() }
	cmd.Flag.Parse(args)
	return cmd.Flag.Args()
}

func (cmd *volumeShowCommand) run(args []string) error {
	if cmd.volume == "" {
		errorf("missing required -volume parameter")
		cmd.usage()
	}

	client, err := storageServiceClient(*identityUser, *identityPassword, *tenantID)
	if err != nil {
		fatalf("Could not get volume service client [%s]\n", err)
	}

	volume, err := volumes.Get(client, cmd.volume).Extract()
	if err != nil {
		return err
	}

	dumpVolume(volume)
	return nil
}

type volumeUpdateCommand struct {
	Flag        flag.FlagSet
	volume      string
	name        string
	description string
}

func (cmd *volumeUpdateCommand) usage(...string) {
	fmt.Fprintf(os.Stderr, `usage: ciao-cli [options] volume update [flags]

Updates a volume

The update flags are:
`)
	cmd.Flag.PrintDefaults()
	os.Exit(2)
}

func (cmd *volumeUpdateCommand) parseArgs(args []string) []string {
	cmd.Flag.StringVar(&cmd.volume, "volume", "", "Volume UUID")
	cmd.Flag.StringVar(&cmd.name, "name", "", "Volume name")
	cmd.Flag.StringVar(&cmd.description, "description", "", "Volume description")
	cmd.Flag.Usage = func() { cmd.usage() }
	cmd.Flag.Parse(args)
	return cmd.Flag.Args()
}

func (cmd *volumeUpdateCommand) run(args []string) error {
	if cmd.volume == "" {
		errorf("missing required -volume parameter")
		cmd.usage()
	}

	client, err := storageServiceClient(*identityUser, *identityPassword, *tenantID)
	if err != nil {
		fatalf("Could not get volume service client [%s]\n", err)
	}

	opts := volumes.UpdateOpts{
		Name:        cmd.name,
		Description: cmd.description,
	}

	vol, err := volumes.Update(client, cmd.volume, opts).Extract()
	if err == nil {
		fmt.Printf("Updated volume: %s\n", vol.ID)
	}
	return err
}

type volumeDeleteCommand struct {
	Flag   flag.FlagSet
	volume string
}

func (cmd *volumeDeleteCommand) usage(...string) {
	fmt.Fprintf(os.Stderr, `usage: ciao-cli [options] volume delete [flags]

Deletes a volume

The delete flags are:
`)
	cmd.Flag.PrintDefaults()
	os.Exit(2)
}

func (cmd *volumeDeleteCommand) parseArgs(args []string) []string {
	cmd.Flag.StringVar(&cmd.volume, "volume", "", "Volume UUID")
	cmd.Flag.Usage = func() { cmd.usage() }
	cmd.Flag.Parse(args)
	return cmd.Flag.Args()
}

func (cmd *volumeDeleteCommand) run(args []string) error {
	if cmd.volume == "" {
		errorf("missing required -volume parameter")
		cmd.usage()
	}

	client, err := storageServiceClient(*identityUser, *identityPassword, *tenantID)
	if err != nil {
		fatalf("Could not get volume service client [%s]\n", err)
	}

	err = volumes.Delete(client, cmd.volume).ExtractErr()
	if err == nil {
		fmt.Printf("Deleted volume: %s\n", cmd.volume)
	}
	return err
}

func storageServiceClient(username, password, tenant string) (*gophercloud.ServiceClient, error) {
	opt := gophercloud.AuthOptions{
		IdentityEndpoint: *identityURL + "/v3/",
		Username:         username,
		Password:         password,
		DomainID:         "default",
		TenantID:         tenant,
		AllowReauth:      true,
	}

	provider, err := openstack.AuthenticatedClient(opt)
	if err != nil {
		errorf("Could not get AuthenticatedClient %s\n", err)
	}

	return openstack.NewBlockStorageV2(provider, gophercloud.EndpointOpts{
		Name:   "cinderv2",
		Region: "RegionOne",
	})
}

func dumpVolume(v *volumes.Volume) {
	fmt.Printf("\tName             [%s]\n", v.Name)
	fmt.Printf("\tSize             [%d GB]\n", v.Size)
	fmt.Printf("\tUUID             [%s]\n", v.ID)
	fmt.Printf("\tStatus           [%s]\n", v.Status)
	fmt.Printf("\tDescription      [%s]\n", v.Description)
}
