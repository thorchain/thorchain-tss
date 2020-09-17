# THORCHain TSS

****

> **Mirror**
>
> This repo mirrors from THORChain Gitlab to Github. 
> To contribute, please contact the team and commit to the Gitlab repo:
>
> https://gitlab.com/thorchain/tss/go-tss


****

## How to play

* modilfy the docker-compose file at build/docker-compose.yml according to your local paths.

* run the docker-compose, itl will create 4 parties with threshold of 2.


### How to contribute

* Create an issue or find an existing issue on https://gitlab.com/thorchain/tss/go-tss/-/issues
* Assign the issue to yourself
* Create a branch using the issue id, for example if the issue you are working on is 600, then create a branch call `600-issue` , this way , gitlab will link your PR with the issue
* Raise a PR , and submit it for the team to review
* Make sure the pipeline is green
* Once PR get approved, you can merge it to master

### the semantic version and release
Go-TSS manage changelog entry the same way like gitlab, refer to (https://docs.gitlab.com/ee/development/changelog.html) for more detail. Once a merge request get merged into master branch,
if the merge request upgrade the [version](https://gitlab.com/thorchain/tss/go-tss/-/blob/master/version), then a new release will be created automatically, and the repository will be tagged with
the new version by the release tool.

### How to generate a changelog entry
A scripts/changelog  is available to generate the changelog entry file automatically.

Its simplest usage is to provide the value for title:
```
./scripts/changelog "my super amazing change"
```
At this point the script would ask you to select the category of the change (mapped to the type field in the entry):
```bash
>> Please specify the category of your change:
1. New feature
2. Bug fix
3. Feature change
4. New deprecation
5. Feature removal
6. Security fix
7. Performance improvement
8. Other
```
The entry filename is based on the name of the current Git branch. If you run the command above on a branch called feature/hey-dz, it will generate a changelogs/unreleased/feature-hey-dz.yml file.

The command will output the path of the generated file and its contents:
```
create changelogs/unreleased/my-feature.yml
---
title: Hey DZ, I added a feature to GitLab!
merge_request:
author:
type:
```

#### Arguments
|Argument|	Shorthand|	Purpose|
|---|---|---|
|--amend| |	 	Amend the previous commit|
|--force|	-f|	Overwrite an existing entry|
|--merge-request|	-m|	Set merge request ID|
|--dry-run|	-n|	Don’t actually write anything, just print|
|--git-username|	-u|	Use Git user.name configuration as the author|
|--type|	-t|	The category of the change, valid options are: added, fixed, changed, deprecated, removed, security, performance, other|
|--help|	-h|	Print help message|

##### --amend
You can pass the --amend argument to automatically stage the generated file and amend it to the previous commit.

If you use --amend and don’t provide a title, it will automatically use the “subject” of the previous commit, which is the first line of the commit message:
```
$ git show --oneline
ab88683 Added an awesome new feature to GitLab

$ scripts/changelog --amend
create changelogs/unreleased/feature-hey-dz.yml
---
title: Added an awesome new feature to GitLab
merge_request:
author:
type:
```
#### --force or -f
Use --force or -f to overwrite an existing changelog entry if it already exists.

```
$ scripts/changelog 'Hey DZ, I added a feature to GitLab!'
error changelogs/unreleased/feature-hey-dz.yml already exists! Use `--force` to overwrite.

$ scripts/changelog 'Hey DZ, I added a feature to GitLab!' --force
create changelogs/unreleased/feature-hey-dz.yml
---
title: Hey DZ, I added a feature to GitLab!
merge_request: 1983
author:
type:
```

####--merge-request or -m
Use the --merge-request or -m argument to provide the merge_request value:

```
$ scripts/changelog 'Hey DZ, I added a feature to GitLab!' -m 1983
create changelogs/unreleased/feature-hey-dz.yml
---
title: Hey DZ, I added a feature to GitLab!
merge_request: 1983
author:
type:
```

#### --dry-run or -n
Use the --dry-run or -n argument to prevent actually writing or committing anything:

```
$ scripts/changelog --amend --dry-run
create changelogs/unreleased/feature-hey-dz.yml
---
title: Added an awesome new feature to GitLab
merge_request:
author:
type:

$ ls changelogs/unreleased/
```

#### --git-username or -u
Use the --git-username or -u argument to automatically fill in the author value with your configured Git user.name value:

```
$ git config user.name
Jane Doe

$ scripts/changelog -u 'Hey DZ, I added a feature to GitLab!'
create changelogs/unreleased/feature-hey-dz.yml
---
title: Hey DZ, I added a feature to GitLab!
merge_request:
author: Jane Doe
type:
```

#### --type or -t
Use the --type or -t argument to provide the type value:

```
$ bin/changelog 'Hey DZ, I added a feature to GitLab!' -t added
create changelogs/unreleased/feature-hey-dz.yml
---
title: Hey DZ, I added a feature to GitLab!
merge_request:
author:
type: added
```


