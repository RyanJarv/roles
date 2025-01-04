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

Plugins can be put in [./pkg/plugins](./pkg/plugins). These should implement the plugin.Plugin interface:

```
type Plugin interface {
	Name() string
	Setup(ctx *utils.Context) error
	ScanArn(ctx *utils.Context, arn string) (bool, error)
	CleanUp(ctx *utils.Context) error
}
```

And have a initializer function of:

> func New<ResourceType>(cfgs map[string]aws.Config, concurrency int, input New<ResourceType>Input) []Plugin

The initializer funtion takes a map of region names to aws.Config's, one for each active region in the account and should return new SNS plugin for each region/thread.

```
// NewSNSTopics creates a new SNS plugin for each region/thread.
func NewSNSTopics(cfgs map[string]aws.Config, concurrency int, input NewSNSInput) []Plugin {
	var results []Plugin

	for region, cfg := range cfgs {
		// Create a single sns.Client per region
		snsClient := sns.NewFromConfig(cfg)

		for i := 0; i < concurrency; i++ {
			topicName := fmt.Sprintf("role-fh9283f-sns-%s-%s-%d", region, input.AccountId, i)

			results = append(results, &SNSTopic{
				NewSNSInput: input,
				thread:      i,
				region:      region,
				topicName:   topicName,
				topicArn:    fmt.Sprintf("arn:aws:sns:%s:%s:%s", region, input.AccountId, topicName),
				snsClient:   snsClient,
			})
		}
	}

	return results
}
```

The initializer function must create all variables consumed in all other methods, we can not rely on `Setup` being called before any other method.

The `Setup` function creates any necessary infrastructure needed to call the other methods, however the `Setup` method is only called when the `-setup` flag is passed on
the command line. If an ARN is needed in another method, the initializer must construct the ARN deterministicly before `Setup` is called. For example, if the ARN of a SNS
topic is needed by the `ScanArn` function to call `SetTopicAttributes`, the ARN should be constructed manually during initialization with:

> fmt.Sprintf("arn:aws:sns:%s:%s:%s", region, input.AccountId, topicName)

Then when `Setup` is called it will create the `topicName` SNS Topic. This way the `Setup` method doesn't need to be called during every run.

Each plugin thread should create it's own resource to avoid affecting other threads, so in the SNS case above, `topicName` may be something like:

> topicName := fmt.Sprintf("role-fh9283f-sns-%s-%s-%d", region, input.AccountId, thread)

And each call to `ScanArn` will only update the topic attributes of it's own SNS topic.

The `ScanArn` method takes a given principal ARN as a string and returns true if the principal ARN exists, and false if it doesn't. It does this by updating the
resource policy of the resource owned by the current thread to the policy generated from a call to `GenerateTrustPolicy`:

```
func GenerateTrustPolicy(resourceArn, action, principalArn string) PolicyDocument {
	return PolicyDocument{
		Version: "2012-10-17",
		Statement: []PolicyStatement{
			{
				Sid:      "testrole",
				Effect:   "Deny",
				Action:   action,
				Resource: resourceArn,
				Principal: PolicyPrincipal{
					AWS: principalArn,
				},
			},
		},
	}
}
```

GenerateTrustPolicy takes the `resourceArn` of the current thread's Resource which will be updated, a valid action, and the principalArn which is currently being
scanned. Each resource behaves slightly differently, but updating the resource policy will return succesfully if the specified `principalArn` exists, and a specific
error if it does not.

ChatGPT can generate these plugins for you by passing this description and an example plugin.

## Build

```
make build
./build/roles -help
```
