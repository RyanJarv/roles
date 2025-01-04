# Roles

Unauthenticated enumeration of AWS IAM Roles.

Currently around 1700 roles/second in EC2 w/ a single account.

## Usage

```
make build
./build/darwin-arm/roles -profile scanner -setup
./build/darwin-arm/roles -profile scanner -account-list ./path/to/accounts.list -roles ~/path/to/role_names.list
```

## Lists

The account and role names lists are just plain lists with one value per line with an optional comment.

White space is trimmed from the beginning and end of the value before it is used.

### Roles List

* The role names list are the names or path + role name without the `role/` prefix.
* The path passed to `-roles` can be a directory containing a number of role name lists with the `.list` file extension.
* Role names can be GoLang templates which contain `{{.AccountId}}` or `{{.Region}}` which get replaced with the current account ID or region being scanned.

For example:

```
StaticRoleName # Default X role # Found at ...
DynamicRoleName-{{.Region}}-{{.AccountId}} # Software A # Found at ...
path/DynamicRoleName-{{.Region}}-{{.AccountId}} # Software B # Found at ...
```

## Plugins

Can be put in [./pkg/plugins](./pkg/plugins).

## Build

```
make build
./build/roles -help
```
