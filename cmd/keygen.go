// Copyright © 2017 nikkolasg
//
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

import "github.com/spf13/cobra"

var IdentityFolder = "identity"
var GroupFolder = "groups"

// keygenCmd represents the keygen command
var keygenCmd = &cobra.Command{
	Use:   "keygen",
	Short: "keygen is the root command to generate dist. or local keypair",
	Long:  "keygen is the root command that enables to create a local ed25519 key pair, or a distributed key pair.",
}

func init() {
	RootCmd.AddCommand(keygenCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// keygenCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// keygenCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")

}