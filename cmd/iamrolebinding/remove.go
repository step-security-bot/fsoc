// Copyright 2023 Cisco Systems, Inc.
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

package iamrolebinding

import (
	"github.com/apex/log"
	"github.com/spf13/cobra"

	"github.com/cisco-open/fsoc/output"
)

// iamRbCmd represents the role binding command group
var iamRbRemoveCmd = &cobra.Command{
	Use:   "remove <principal> [<role>]+",
	Short: "Remove roles from a principal",
	Long: `Remove one or more roles from a principal that has the roles. 
	
To the see roles bound to a principal, use "list" command; to see permissions for a given role, use the "iam-role permissions" command.
	
This command requires a principal with tenant administrator access.`,
	Example: `
  fsoc rb remove riker@example.com iam:tenantAdmin spacefleet:commandingOfficer
  fsoc rb remove srv_1ZGdlbcm8NajPxY4o43SNv optimize:optimizationManager`,
	Args: cobra.MinimumNArgs(2),
	Run:  removeRoles,
}

// Package registration function for the iam-role-binding command root
func newCmdRbRemove() *cobra.Command {
	return iamRbRemoveCmd
}

func removeRoles(cmd *cobra.Command, args []string) {
	if err := patchRoles(args[0], args[1:], false); err != nil {
		log.Fatal(err.Error())
	}

	output.PrintCmdStatus(cmd, "Roles removed successfully.\n")
}
