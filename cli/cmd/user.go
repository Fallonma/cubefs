// Copyright 2018 The CubeFS Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/cubefs/cubefs/proto"
	"github.com/cubefs/cubefs/sdk/master"
	"github.com/spf13/cobra"
)

const (
	cmdUserUse   = "user [COMMAND]"
	cmdUserShort = "Manage cluster users"
)

func newUserCmd(client *master.MasterClient) *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdUserUse,
		Short: cmdUserShort,
		Args:  cobra.MinimumNArgs(0),
	}
	cmd.AddCommand(
		newUserCreateCmd(client),
		newUserInfoCmd(client),
		newUserListCmd(client),
		newUserPermCmd(client),
		newUserUpdateCmd(client),
		newUserDeleteCmd(client),
	)
	return cmd
}

const (
	cmdUserCreateUse   = "create [USER ID]"
	cmdUserCreateShort = "Create a new user"
)

func newUserCreateCmd(client *master.MasterClient) *cobra.Command {
	var optPassword string
	var optAccessKey string
	var optSecretKey string
	var optUserType string
	var clientIDKey string
	var optYes bool
	cmd := &cobra.Command{
		Use:   cmdUserCreateUse,
		Short: cmdUserCreateShort,
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			userID := args[0]
			password := optPassword
			accessKey := optAccessKey
			secretKey := optSecretKey
			userType := proto.UserTypeFromString(optUserType)
			defer func() {
				errout(err)
			}()
			if !userType.Valid() {
				err = fmt.Errorf("Invalid user type. ")
				return
			}

			// ask user for confirm
			if !optYes {
				// display information before create
				displayPassword := "[default]"
				if optPassword != "" {
					displayPassword = optPassword
				}
				displayAccessKey := "[auto generate]"
				displaySecretKey := "[auto generate]"
				if optAccessKey != "" {
					displayAccessKey = optAccessKey
				}
				if optSecretKey != "" {
					displaySecretKey = optSecretKey
				}
				displayUserType := userType.String()
				fmt.Printf("Create a new CubeFS cluster user\n")
				stdout("  User ID   : %v\n", userID)
				stdout("  Password  : %v\n", displayPassword)
				stdout("  Access Key: %v\n", displayAccessKey)
				stdout("  Secret Key: %v\n", displaySecretKey)
				stdout("  Type      : %v\n", displayUserType)
				stdout("\nConfirm (yes/no)[yes]: ")
				var userConfirm string
				_, _ = fmt.Scanln(&userConfirm)
				if userConfirm != "yes" && len(userConfirm) != 0 {
					err = fmt.Errorf("Abort by user.\n")
					return
				}
			}

			param := proto.UserCreateParam{
				ID:        userID,
				Password:  password,
				AccessKey: accessKey,
				SecretKey: secretKey,
				Type:      userType,
			}
			var userInfo *proto.UserInfo
			if userInfo, err = client.UserAPI().CreateUser(context.TODO(), &param, clientIDKey); err != nil {
				err = fmt.Errorf("Create user failed: %v\n", err)
				return
			}

			// display operation result
			stdout("Create user success:\n")
			printUserInfo(userInfo)
		},
	}
	cmd.Flags().StringVar(&optPassword, "password", "", "Specify user password")
	cmd.Flags().StringVar(&optAccessKey, "access-key", "", "Specify user access key for object storage interface authentication [16 digits & letters]")
	cmd.Flags().StringVar(&optSecretKey, "secret-key", "", "Specify user secret key for object storage interface authentication [32 digits & letters]")
	cmd.Flags().StringVar(&optUserType, "user-type", "normal", "Specify user type [normal | admin]")
	cmd.Flags().StringVar(&clientIDKey, CliFlagClientIDKey, client.ClientIDKey(), CliUsageClientIDKey)
	cmd.Flags().BoolVarP(&optYes, "yes", "y", false, "Answer yes for all questions")
	return cmd
}

const (
	cmdUserUpdateUse   = "update [USER ID]"
	cmdUserUpdateShort = "Update information about specified user"
)

func newUserUpdateCmd(client *master.MasterClient) *cobra.Command {
	var optAccessKey string
	var optSecretKey string
	var optUserType string
	var clientIDKey string
	var optYes bool
	cmd := &cobra.Command{
		Use:   cmdUserUpdateUse,
		Short: cmdUserUpdateShort,
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			userID := args[0]
			accessKey := optAccessKey
			secretKey := optSecretKey
			var userType proto.UserType
			defer func() {
				errout(err)
			}()
			if optUserType != "" {
				userType = proto.UserTypeFromString(optUserType)
				if !userType.Valid() {
					err = fmt.Errorf("Invalid user type ")
					return
				}
			}

			if !optYes {
				displayAccessKey := "[no change]"
				if optAccessKey != "" {
					displayAccessKey = optAccessKey
				}
				displaySecretKey := "[no change]"
				if optSecretKey != "" {
					displaySecretKey = optSecretKey
				}
				displayUserType := "[no change]"
				if optUserType != "" {
					displayUserType = optUserType
				}
				fmt.Printf("Update CubeFS cluster user\n")
				stdout("  User ID   : %v\n", userID)
				stdout("  Access Key: %v\n", displayAccessKey)
				stdout("  Secret Key: %v\n", displaySecretKey)
				stdout("  Type      : %v\n", displayUserType)
				stdout("\nConfirm (yes/no)[yes]: ")
				var userConfirm string
				_, _ = fmt.Scanln(&userConfirm)
				if userConfirm != "yes" && len(userConfirm) != 0 {
					err = fmt.Errorf("Abort by user.\n")
					return
				}
			}
			if accessKey == "" && secretKey == "" && optUserType == "" {
				err = fmt.Errorf("no update")
				return
			}
			param := proto.UserUpdateParam{
				UserID:    userID,
				AccessKey: accessKey,
				SecretKey: secretKey,
				Type:      userType,
			}
			var userInfo *proto.UserInfo
			if userInfo, err = client.UserAPI().UpdateUser(context.TODO(), &param, clientIDKey); err != nil {
				return
			}

			stdout("Update user success:\n")
			printUserInfo(userInfo)
		},
	}
	cmd.Flags().StringVar(&optAccessKey, "access-key", "", "Update user access key")
	cmd.Flags().StringVar(&optSecretKey, "secret-key", "", "Update user secret key")
	cmd.Flags().StringVar(&optUserType, "user-type", "", "Update user type [normal | admin]")
	cmd.Flags().StringVar(&clientIDKey, CliFlagClientIDKey, client.ClientIDKey(), CliUsageClientIDKey)
	cmd.Flags().BoolVarP(&optYes, "yes", "y", false, "Answer yes for all questions")
	return cmd
}

const (
	cmdUserDeleteUse   = "delete [USER ID]"
	cmdUserDeleteShort = "Delete specified user"
)

func newUserDeleteCmd(client *master.MasterClient) *cobra.Command {
	var optYes bool
	// var optForce bool
	var clientIDKey string
	cmd := &cobra.Command{
		Use:   cmdUserDeleteUse,
		Short: cmdUserDeleteShort,
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			userID := args[0]
			defer func() {
				errout(err)
			}()
			if !optYes {
				stdout("Delete user [%v] (yes/no)[no]:", userID)
				var userConfirm string
				_, _ = fmt.Scanln(&userConfirm)
				if userConfirm != "yes" {
					err = fmt.Errorf("Abort by user.\n")
					return
				}
			}

			if err = client.UserAPI().DeleteUser(context.TODO(), userID, clientIDKey); err != nil {
				err = fmt.Errorf("Delete user failed:\n%v\n", err)
				return
			}
			stdout("Delete user success.\n")
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return validUsers(client, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
	}
	cmd.Flags().BoolVarP(&optYes, "yes", "y", false, "Answer yes for all questions")
	cmd.Flags().StringVar(&clientIDKey, CliFlagClientIDKey, client.ClientIDKey(), CliUsageClientIDKey)
	// cmd.Flags().BoolVarP(&optForce, "force", "f", false, "Force to delete user")
	return cmd
}

const (
	cmdUserInfoUse   = "info [USER ID]"
	cmdUserInfoShort = "Show detail information about specified user"
)

func newUserInfoCmd(client *master.MasterClient) *cobra.Command {
	cmd := &cobra.Command{
		Use:   cmdUserInfoUse,
		Short: cmdUserInfoShort,
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			userID := args[0]
			var userInfo *proto.UserInfo
			defer func() {
				errout(err)
			}()
			if userInfo, err = client.UserAPI().GetUserInfo(context.TODO(), userID); err != nil {
				err = fmt.Errorf("Get user info failed: %v\n", err)
				return
			}
			printUserInfo(userInfo)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return validUsers(client, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
	}

	return cmd
}

const (
	cmdUserPermUse   = "perm [USER ID] [VOLUME] [PERM (READONLY,RO,READWRITE,RW,NONE)]"
	cmdUserPermShort = "Setup volume permission for a user"
)

func newUserPermCmd(client *master.MasterClient) *cobra.Command {
	var subdir string
	var clientIDKey string
	cmd := &cobra.Command{
		Use:   cmdUserPermUse,
		Short: cmdUserPermShort,
		Args:  cobra.MinimumNArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			var err error
			userID := args[0]
			volume := args[1]
			var perm proto.Permission
			defer func() {
				errout(err)
			}()

			perm = proto.BuiltinPermissionPrefix
			if subdir != "" && subdir != "/" {
				perm = proto.Permission(string(perm) + subdir + ":")
			}

			switch strings.ToLower(args[2]) {
			case "ro", "readonly":
				perm = perm + "ReadOnly"
			case "rw", "readwrite":
				perm = perm + "Writable"
			case "none":
				perm = proto.NonePermission
			default:
				err = fmt.Errorf("Permission must be on of ro, rw, none ")
				return
			}
			stdout("Setup volume permission\n")
			stdout("  User ID   : %v\n", userID)
			stdout("  Volume    : %v\n", volume)
			stdout("  Subdir    : %v\n", subdir)
			stdout("  Permission: %v\n", perm.ReadableString())

			// ask user for confirm
			stdout("\nConfirm (yes/no)[yes]: ")
			var userConfirm string
			_, _ = fmt.Scanln(&userConfirm)
			if userConfirm != "yes" && len(userConfirm) != 0 {
				err = fmt.Errorf("Abort by user.\n")
				return
			}
			var userInfo *proto.UserInfo
			if _, err = client.UserAPI().GetUserInfo(context.TODO(), userID); err != nil {
				return
			}
			if perm.IsNone() {
				param := proto.NewUserPermRemoveParam(userID, volume)
				userInfo, err = client.UserAPI().RemovePolicy(context.TODO(), param, clientIDKey)
			} else {
				param := proto.NewUserPermUpdateParam(userID, volume)
				param.SetPolicy(perm.String())
				userInfo, err = client.UserAPI().UpdatePolicy(context.TODO(), param, clientIDKey)
			}
			if err != nil {
				return
			}
			printUserInfo(userInfo)
		},
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) != 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return validUsers(client, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
	}
	cmd.Flags().StringVar(&subdir, "subdir", "", "Subdir")
	cmd.Flags().StringVar(&clientIDKey, CliFlagClientIDKey, client.ClientIDKey(), CliUsageClientIDKey)
	return cmd
}

const (
	cmdUserListShort = "List cluster users"
)

func newUserListCmd(client *master.MasterClient) *cobra.Command {
	var optKeyword string
	cmd := &cobra.Command{
		Use:     CliOpList,
		Short:   cmdUserListShort,
		Aliases: []string{"ls"},
		Run: func(cmd *cobra.Command, args []string) {
			var users []*proto.UserInfo
			var err error
			defer func() {
				errout(err)
			}()
			if users, err = client.UserAPI().ListUsers(context.TODO(), optKeyword); err != nil {
				return
			}
			stdout("%v\n", userInfoTableHeader)
			for _, user := range users {
				stdout("%v\n", formatUserInfoTableRow(user))
			}
		},
	}
	cmd.Flags().StringVar(&optKeyword, "keyword", "", "Specify keyword of user name to filter")
	return cmd
}

func printUserInfo(userInfo *proto.UserInfo) {
	stdout("[Summary]\n")
	stdout("  User ID    : %v\n", userInfo.UserID)
	stdout("  Access Key : %v\n", userInfo.AccessKey)
	stdout("  Secret Key : %v\n", userInfo.SecretKey)
	stdout("  Type       : %v\n", userInfo.UserType)
	stdout("  Create Time: %v\n", userInfo.CreateTime)
	if userInfo.Policy == nil {
		return
	}
	stdout("[Volumes]\n")
	stdout("%-20v    %-12v\n", "VOLUME", "PERMISSION")
	for _, vol := range userInfo.Policy.OwnVols {
		stdout("%-20v    %-12v\n", vol, "Owner")
	}
	for vol, perms := range userInfo.Policy.AuthorizedVols {
		stdout("%-20v    %-12v\n", vol, strings.Join(perms, ","))
	}
}
