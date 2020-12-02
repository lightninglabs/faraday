# Loop Client Release Notes
This file tracks release notes for the loop client. 

### Developers: 
* When new features are added to the repo, a short description of the feature should be added under the "Next Release" heading.
* This should be done in the same PR as the change so that our release notes stay in sync!

### Release Manager: 
* All of the items under the "Next Release" heading should be included in the release notes.
* As part of the PR that bumps the client version, cut everything below the 'Next Release' heading. 
* These notes can either be pasted in a temporary doc, or you can get them from the PR diff once it is merged. 
* The notes are just a guideline as to the changes that have been made since the last release, they can be updated.
* Once the version bump PR is merged and tagged, add the release notes to the tag on GitHub.

## Next release

#### New Features
* The output of the `audit` rpc is now sorted by ascending timestamp. 
* A pre-set custom category for [Lightning Pool](https://github.com/lightninglabs/pool) has been added to the `audit` cli, and can be used to separate all pool-related transactions into their own category called `pool` using `audit --pool-category`.

#### Breaking Changes

#### Bug Fixes
* A bug in the `audit` custom categories functionality which switched on-chain and off-chain categories has been fixed. 