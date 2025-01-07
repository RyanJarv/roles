#!/usr/bin/env python3
import argparse
import json
from concurrent.futures.thread import ThreadPoolExecutor
from functools import partial

import boto3


def main(profile: str):
    sess = boto3.Session(profile_name=profile)

    sts = sess.client('sts', region_name='us-east-1')
    root_account_id = sts.get_caller_identity()['Account']

    paginator = sess.client('organizations').get_paginator('list_accounts')
    resp = paginator.paginate()
    for page in resp:
        for account in page['Accounts']:
            if account['Status'] != 'ACTIVE':
                print(f"Skipping account {account['Id']} with status {account['Status']}")
                continue

            if account['Id'] == root_account_id:
                continue
                print(f"[INFO] cleaning root account")
                cleanup_account(sess)
                print(f"[INFO] finished with root account")
            else:
                resp = sess.client('sts').get_caller_identity()
                print(json.dumps(resp, indent=2, default=str))
                print(f"[INFO] cleaning account {account['Id']}")
                clean_up_global(sess)
                cleanup_account(sess, role_arn=f"arn:aws:iam::{account['Id']}:role/OrganizationAccountAccessRole")
                print(f"[INFO] finished with {account['Id']}")


def cleanup_account(sess, role_arn):
    ec2 = sess.client('ec2')
    for name in ThreadPoolExecutor(max_workers=30).map(
        partial(clean_up_region, sess, role_arn),
        [r['RegionName'] for r in ec2.describe_regions()['Regions']],
    ):
        print(f"Finished cleaning up {name}")

    # for r in ec2.describe_regions(AllRegions=True)['Regions']:
    #     clean_up_region(sess, r['RegionName'])

def delete_bucket(s3, bucket: dict):
    try:
        s3.delete_bucket(Bucket=bucket['Name'])
    except Exception as e:
        return bucket['Name'], e

    return bucket['Name'], None


def delete_repo(ecr_public, repo: dict):
    try:
        ecr_public.delete_repository(repositoryName=repo['repositoryName'], force=True)
    except Exception as e:
        return repo['repositoryName'], e
    return repo['repositoryName'], None

def delete_sns_topic(sns, topic):
    try:
        sns.delete_topic(TopicArn=topic['TopicArn'])
    except Exception as e:
        return topic['TopicArn'], e
    return topic['TopicArn'], None

def delete_sqs_queue(sqs, queue_url: str):
    try:
        sqs.delete_queue(QueueUrl=queue_url)
    except Exception as e:
        return queue_url, e
    return queue_url, None


def delete_access_point(access_point, account_id, s3control):
    try:
        s3control.delete_access_point(AccountId=account_id, Name=access_point['Name'])
    except Exception as e:
        return access_point['Name'], e
    return access_point['Name'], None


def clean_up_global(sess):
    s3 = sess.client('s3', region_name='us-east-1')
    resp = s3.list_buckets()

    # for result, err in ThreadPoolExecutor(max_workers=20).map(
    #         partial(delete_bucket, s3),
    #         resp['Buckets'],
    # ):
    #     if err:
    #         print(f"Failed to delete bucket {result}: {err}")
    #     else:
    #         print(f"Deleted bucket {result}")

    ecr_public = sess.client('ecr-public', region_name='us-east-1')
    resp = ecr_public.describe_repositories()

    for result, err in ThreadPoolExecutor(max_workers=20).map(
            partial(delete_repo, ecr_public),
            resp['repositories'],
    ):
        if err:
            print(f"Failed to delete bucket {result}: {err}")
        else:
            print(f"Deleted bucket {result}")



def clean_up_region(sess, region: str):
    print(f"[INFO] cleaning up region {region}")
    sns = sess.client('sns', region_name=region)
    resp = sns.list_topics()

    for name, err in ThreadPoolExecutor(max_workers=20).map(
        partial(delete_sns_topic, sns),
        resp['Topics'],
    ):
        if err:
            print(f"Failed to delete topic {name}: {err}")
        else:
            print(f"Deleted topic {name}")

    sqs = sess.client('sqs', region_name=region)
    resp = sqs.list_queues()
    for name, err in ThreadPoolExecutor(max_workers=20).map(
        partial(delete_sqs_queue, sqs),
        resp.get('QueueUrls', []),
    ):
        if err:
            print(f"Failed to delete queue {name}: {err}")
        else:
            print(f"Deleted queue {name}")


    account_id = sess.client('sts').get_caller_identity()['Account']

    s3control = sess.client('s3control', region_name=region)
    resp = s3control.list_access_points(AccountId=account_id)
    for name, err in ThreadPoolExecutor(max_workers=20).map(
        partial(delete_access_point, account_id=account_id, s3control=s3control),
        resp['AccessPointList'],
    ):
        if err:
            print(f"Failed to delete access point {name}: {err}")
        else:
            print(f"Deleted access point {name}")

    return region


def assume_role_session(sess, account: str, region: str):
    sts = sess.client('sts', region_name=region)
    resp = sts.assume_role(
        RoleArn=f"arn:aws:iam::{account['Id']}:role/OrganizationAccountAccessRole",
        RoleSessionName='cleanup',
        DurationSeconds=3600,
    )

        # Create a new boto3 session using the temporary credentials
    sess = boto3.Session(
        aws_access_key_id=resp['Credentials']["AccessKeyId"],
        aws_secret_access_key=resp['Credentials']["SecretAccessKey"],
        aws_session_token=resp['Credentials']["SessionToken"],
        region_name=region,
    )
    got_account_id = sess.client('sts').get_caller_identity()['Account']
    if got_account_id != account:
        raise UserWarning(f"[ERROR] Failed to assume role for {account}")

    return sess



if __name__ == '__main__':
    parser = argparse.ArgumentParser(
        prog='clean_up',
        description='Clean up misconfigured accounts',
    )
    parser.add_argument('--profile', help='AWS profile name', type=str)

    args = parser.parse_args()

    main(profile=args.profile)
