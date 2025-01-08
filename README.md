# Roles

Unauthenticated enumeration of AWS IAM Roles.

By default, this tool is rate limited to 10 roles/second, this can be increased up to 50 by passing the `-rate` flag.

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

## Organization Setup

**Org setup is not supported currently**

This is documented here for completeness, but I have to recommend against using this. It's really just too fast, you 
do not need it to reach the rate limit of 50 roles/second, and honestly I wouldn't be surprised if AWS shuts down, or
restricts your org if you run this too long. The rate limit of 50 is set because it's approximately the documented 
per-account speed of the other more-commonly known tool for this purpose ([quiet-riot](https://github.com/righteousgambit/quiet-riot)).

If you pass the `-org` with `-setup` this tool assumes it is running in an organization dedicated to running this tool
and nothing else. This will, create an AWS organization in the current account if it doesn't already exist, create a 
number of sub-accounts in the organization with the tag `"role-scanning-account": "true"`, and enable all regions in all
sub-accounts.

### Organization Setup Benchmarks

With the [Organization Setup](#organization-setup) enabled, running on a c6g.2xlarge arm64 instance in us-east-1, with
the SNS and SQS [plugins enabled](./pkg/plugins/main.go): 

* 1 concurrency per region, per account, per plugin
    * `[INFO] processed 74955 in 5.0 seconds: 7360.2/second`
    * `[INFO] processed 169675 in 20.0 seconds: 7456.5/second`
* 2 concurrency per region, per account, per plugin
    * `[INFO] processed 202789 in 10.0 seconds: 13408.0/second`

So about 13,400, although if you do the math that's actually 20,279/second. There was a bug in the stats counter, so the
20k a second looks like the right number (bug fixed [here](https://github.com/RyanJarv/roles/commit/224a2b117ec71f460ab72c60c5533f90b27a8fec) if you want to 
double-check it).

However, a few things worth noting here: 

* This was an unoptimized test.
* Depending on how rate limiting works, these rates may not be representative of a longer run.

## Build

```
make build
./build/roles -help
```



## Plugins

The info below is mostly for passing to ChatGPT to generate new plugins. Just make sure to add the SNS plugin example to the end and update the first line with the plugin you want. 
You'll want to put the new plugin in [./pkg/plugins](./pkg/plugins) and add the initializer function [here](https://github.com/RyanJarv/roles/blob/738d61ec197113e4d0d57664e46b4016527867d1/main.go#L86).

```
Based on the plugin description below, generate a plugin file for ...

Plugins for [github.com/RyanJarv/roles](https://github.com/RyanJarv/roles) should implement the plugin.Plugin interface:

type Plugin interface {
	Name() string
	Setup(ctx *utils.Context) error
	ScanArn(ctx *utils.Context, arn string) (bool, error)
	CleanUp(ctx *utils.Context) error
}

And have a initializer function of:

> func New<ResourceType>(cfgs map[string]aws.Config, concurrency int, input New<ResourceType>Input) []Plugin

The initializer funtion takes a map of region names to aws.Config's, one for each active region in the account and should return new SNS plugin for each region/thread.


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

GenerateTrustPolicy takes the `resourceArn` of the current thread's Resource which will be updated, a valid action, and the principalArn which is currently being
scanned. Each resource behaves slightly differently, but updating the resource policy will return succesfully if the specified `principalArn` exists, and a specific
error if it does not.


Below is an example of the SNS Plugin:


... add SNS plugin code here ...


```
