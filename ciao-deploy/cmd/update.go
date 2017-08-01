// Copyright © 2017 Intel Corporation
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/01org/ciao/ciao-deploy/deploy"
	"github.com/spf13/cobra"
)

// updateCmd represents the update command
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the master node on the cluster",
	Long:  `Use on an already setup master node to update the current software on the node`,
	Run: func(cmd *cobra.Command, args []string) {
		ctx, cancelFunc := context.WithCancel(context.Background())
		defer cancelFunc()

		sigCh := make(chan os.Signal, 1)
		go func() {
			<-sigCh
			cancelFunc()
		}()
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		err := deploy.UpdateMaster(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating master node")
			os.Exit(1)
		}

		if localLauncher {
			err = deploy.SetupLocalLauncher(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error setting up local launcher")
				os.Exit(1)
			}
		}

		os.Exit(0)
	},
}

func init() {
	RootCmd.AddCommand(updateCmd)
	updateCmd.Flags().BoolVar(&localLauncher, "local-launcher", false, "Enable a local launcher on this node (for testing)")

}