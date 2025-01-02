#!/usr/bin/env bash

set -euo pipefail

# TODO(first run): update these
ACCOUNT_ID="12345689012"
ROLE_ARN="arn:aws:iam::${ACCOUNT_ID}:role/test"

line_no() {
  local lineno=$1
  local msg=$2
  echo "Failed at $lineno: $msg"
}
trap 'line_no ${LINENO} "$BASH_COMMAND"' ERR

function info() {
  echo "[INFO] $*" > /dev/stderr
}

function error() {
  echo "[ERROR] $*" > /dev/stderr
}

function fix_cli_input_file() {
  CMD=$1

  while :; do
    if update_cmd_input "$CMD"; then
      info "input updated"
      return
    else
      error "invalid json response"
    fi
  done
}

function describe_change_set() {
  while :; do
    if ! aws cloudformation describe-change-set --profile bb-test --stack-name test --change-set-name preview-change-set > ./changeset; then
      info "successfully described change set"

      names=$(cat ./changeset | jq '.Changes|.[]|select(.Type == "Resource")|.ResourceChange|select(.ResourceType == "AWS::IAM::Role")|.LogicalResourceId' -r)
      echo $names >> ./names.list
      info "found names:" $names


      echo ./changeset
      return
    elif cat ./changeset|jq '.ExecutionStatus' -r|grep -E -q '(CREATE_IN_PROGRESS|CREATE_PENDING)'; then
      info "change set in progress"
      sleep 3
      continue
    else
      error "failed to describe change set"
      continue
    fi
  done
}

function update_cmd_input() {
  CMD=$1

  input=$(
    cat <<EOH
Return a JSON to be written to the `input.json` file which is used in the parameter '--cli-input-json=file://input.json'
of the command shown below.

Important:
  * The whole response MUST be valid JSON. Do NOT include any other text. Do NOT explain anything.
  * The whole response will be written directly to the file.
  * Keys other then Parameters and Capabilities will not be used, do not include them.
  * Ensure placeholder values realistically represent the most common use case.
     * Instead of StackPrefix use a real value like 'prod'.
  * Do not include \`\`\`json or \`\`\` at the beginning or end of the file.
  * When parameters do not exist in the template they must be removed.
  * If an account ID is needed use ${ACCOUNT_ID}.

# Command which we are running:
> ${CMD}

# Currently ./input.json is, this needs to be updated to fix the error below.:
\`\`\`
$(cat ./output)
\`\`\`

# The returned JSON will be written to ./input.json and MUST fix this error:
\`\`\`
$(cat ./error)
\`\`\`

# The first 100 lines of the cloudformation we're using is
\`\`\`
$(head -n 100 $file)
\`\`\`

EOH
)

  info "llm input: $(echo "$input" | sed -E 's/^/    /')"

  resp=$(echo "$input" | llm -c $file)
  echo "$resp" > ./output

  info "llm response: $(echo "$resp" | sed -E 's/^/    /')"

  # only update output if jq succeeds
  if echo "$resp"| jq '{Parameters: .Parameters, Capabilities: .Capabilities}' 2>>./error; then
    echo "$resp" > ./output
    return
  fi

  return 1
}

files=$(grep -R ' RoleName:' files|cut -d: -f1|sort|uniq)
info "found $(echo "$files"|wc -l) files"


for file in $files; do
  if test -f "${file}.changeset"; then
    info "skipping ${file}"
    continue
  else
    info "running $file"
  fi

  input="$(
    aws cloudformation create-change-set --generate-cli-skeleton | jq '{Parameters: .Parameters, Capabilities: .Capabilities}'
  )"

  while :; do
    sleep 2
    aws cloudformation delete-change-set --profile bb-test --stack-name test --change-set-name preview-change-set || :
    sleep 1

    echo "$input" > ./output
    cmd="aws cloudformation create-change-set
      --capabilities CAPABILITY_NAMED_IAM \
      --profile bb-test \
      --stack-name test \
      --template-body file://${file}  \
      --change-set-name preview-change-set  \
      --role-arn  ${ROLE_ARN} \
      --cli-input-json=file://output"

    info "running: $cmd"

    if $cmd 2>./error; then
      describe_change_set > "${file}.changeset"
      break
    else
      error "$(cat ./error)"


      input="$(fix_cli_input_file "$cmd")"
      echo "got input: $input"
    fi

  done
done



