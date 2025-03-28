This is a transcript of a recording about how to install an app in a Giant Swarm Workloadcluster. The app already has a helm chart. Please analyze the transcript.

# Transcript  
Okay, I didn't remember, but Kabbage starts using Vendir for all the… Vendir. Vendir, yeah, which we use it for the last hackathon, the Envoy Gateway, and I even like it more than the sub-module thing, the Git sub-module stuff, because, yeah, it's straightforward. So I found this one. Yes, yes, this is basically where you have to kind of say in which directory you want to include the app, right, and you can also put some constraints, like versions or whatever. If you go a bit down, you will see the example config, which looks like this config, and then you put there where it has to be the path to be updated and everything. So how they do, right, is they put the upstream chart as a dependency, and then you use it within your chart, just the dependency, and you can add, for example, in the helper the labels if you want, something like that. That's one way to do it, probably, it's better than the sub-module part. So how do I need to start? Do I need to create the transform app from the template? Yeah, yeah, you need to create first the… From the template. From the template, the repo, you can call it n8n-app, a high phone app. Yeah. Use this template here? Yeah. In our repository? Yeah. Cool, yeah. So… And then… Transform. Okay, I'm going to say if it's public or not. No, this can be public. And then, yeah. I don't think Renovate… Yeah, well, Renovate can give you, I think, updates on the charts from upstream, so it can help you, yeah. How do we describe that? Describe that? M-chart. I'm going to do M-chart. Great repository. Good. Okay. So, you can clone it. So, I can clone it locally, yeah. Okay. And… Yeah, I've cloned it. So, going back to Rendier. So, the upstream repository, I still fork it? No, you have to just put in the config. You copy, for example, the… But here, it says upstream. The upstream repository is a fork of the original repository. It holds the charts. They use the… They use the… I think this is, if you want to contribute back, to have it always a base repo. I don't think here it's necessary, because what Rendier does is, from the URL you give it, it pulls all the information, and then you just add the chart or whatever you want to update on the right path that you have put, but nothing else. So, I don't think you have to fork it, at least not right now. Okay. So, this… I need something like this here? Yeah, you need something like that. And that's a Rendier YAML inside of the repository? Let me see. Yeah, Rendier YAML, I think it is. Yeah, YAML. Oh, YAML. It's not YAML. All right. And then the vendor is… Hmm. What is that path? So, the first URL is the charts, and the included path is what you want to include from that. So, in your case, it's charts and 8n, right? If you go to the upstream. The upstream repo is here. It was… It was… mcharts, charts and 8n. No. Huh? No, no. Charts, yeah. Charts and then 8n. Yeah. Okay, yeah. Then you have to specify that path on the community chart, mchart. But… But I… Let me share this one. So, here I put the repository, and here… You can put vendor. Vendor. Vendor. Sorry, vendor. You have to put vendor. You have to put vendor in the path. No. Vendor is here. Okay, yeah. The path is… N8n? Where it's going to land. It doesn't really matter, but choose whatever you want. Because it's… You can put N8n, for example. Yeah, yeah, yeah. That makes sense. All right. Okay. Because then in the next path is where you are going to say, okay, from my local vendor N8 folder, copy everything to my helm chart template, for example. And it will update all the templates. Okay. Okay. And the second one, I don't need. Dependents, charts, dependencies. Or let me check if there is anything like that. No. All right. I only know that there are some dependencies regarding Bitnami. But I guess that's linked. But you need them. You need them. Hmm? You need… But they are linked. Yeah, but you are not copying the chart.yaml, right? Or you are. I mean, if you are copying the chart.yaml and overwriting the ask, the problem is that our chart.yaml is… It has a specific format, for example, with the log and everything. So what is doing, for example, Gavit here is copying also the charts dependency, which… Yeah, that could work. Yeah. That works. Yeah. So… But it's a different repository. It's a different repository. Yeah, but it is put in the charts folder. If you go one level up in GitHub. Here? No, it's not. Yeah. You don't have charts. Yeah. You have charts folder, right? And there you have… Ah, but it's not pull. No, no. It's… I mean, the dependency is totally external. Yeah, yeah. But sometimes they build them or push them with the dependency. No, I mean, the repository is another one, right? So if you give that to Helm, it would not pull from the same repository. It would pull from another repository. Hmm. Let me think about that, how it can be done. I guess just copying this from the chart demo, right? There's no other way around. So I copy that into our chart demo? Yeah. I mean, this is the easiest one, yeah. And I don't think dependencies change so often, right? I mean… It's… Yeah. Some maintenance might be necessary because… I mean, Vendyr is not taking care of changing the versions of the dependencies, right? For example, yeah. Yeah. It could be that Vendyr can update. No, it's not. No. I know that BNTech made a script for such cases, but using the submodels. It was trying to parse for updates and other things, but… It's not using this approach. Yeah. Okay. I think I have the Vendyr part ready, then… Then you have to do what is… Well, first, if you have just forked the app template, you have to replace the… The HEM? The HEM app variable, because otherwise it will fail when you build it. So… The template app in the redmi tells you what has to be done. Okay. I think it's just… Where is it? Ah, no. It's the intranet. It's the intranet page. It's this one. How to add? It's in the redmi, but you have to follow it. You can put it in the repo. Okay. First, I need to go to GitHub. GitHub? Yeah, you can use this. I'm giving you… Because we might need to update it in the template, too. But you can run DeepCTL to set up the repo for you instead of… Of going to… Doing it manually. Okay. So the first create and configure your repo can be replaced by DeepCTL. I will try to update this today. No, I don't know how old my DeepCTL is. Is there… Is there an update command for DeepCTL? DeepCTL has an update command. DeepCTL has an update command. Maybe my DeepCTL is too old to have the… Ah, it could be. …loading DeepCTL… …and build it. Okay. Yeah, you can do it manually if you want. It's just setting the right component. But I don't know if it's super up to date, this page, to be honest. I know that DeepCTL does for you. Because, also, you have setting, change it. And now the protection rules works a bit different. But, yeah, it should be similar. Yeah, I'm installing the binary. Yeah. Jumped from 5.15 or something like that to 7.1.2 DeepCTL. So what's the command, DeepCTL? DeepCTL repo setup and the URL. DeepCTL repo setup. DeepCTL repo setup. And then I need repo setup. How does the repository look like? It's without ATTPS. Like, that's the Antron. Only Giant Swarm, N8N app. Yeah. N variable not found. GitHub token was not found. Okay, yeah. You have to put the GitHub token variable. Otherwise, you cannot do it. Okay. I should have the token somewhere. I'm just not sure where, because I cannot find it in my environment. Let me check. I think it's... Do you have tokens generated? Yeah, let me see. I have... GitHub OPCTL token. Yeah, now it works. All right. Completed. Good. Then you can move forward. The resources, because we are using Bendy, is what is different. You can move to 3, which is in SuiteQuality. You have to check the chart.yml. Yeah. But if you just copy from the app, I think it's fine. But how to use AVS? By default, in the app, if you go to the editor, I don't see the ID, but if you share with me the ID, first, you have to change, for sure, the variable, the placeholder they put for the template app. Let me check if I can share my screen. Let me check if I can share my screen. Okay. Now? My Chrome crashed. Okay. Can I try to share? Okay. Let me try once more. Now it works. It works? Yeah. So, I'm here. So, where do I need to change the... So, I see that here it's app name. I guess I need to change something like that. I need to do something here with the template, right? Yeah. The app name should be changed in the other place by n8n. Do I need to do this manually or is there a way to do this? I use my ID for that. I use Visual Code. Because it's also in the EBS.yaml, which is the EBS configuration file, it's also high-fidelity. It would be good to have a command for this. You can use, if I remember correctly, find and replace, like find command. Yeah. Yeah. You can also use z or anything. Or z, yeah. Let me try. To remember. Actually, that would be a nice tool for devs. Yeah. Yeah. Okay. This will work. Okay. I thought that would work here. Let's see the other place. What? Oh, no. What is it? No, it doesn't work. What did you send? Find dot dash type f exec and then the set command that replace everywhere. You have to remove the plus in the end. What? Is it because I am in Mac? I guess so, yeah. Yeah, the set command might be different. Yeah. No, I think it's the find command. Okay. Yeah, I got it. Because I was thinking that it needs to be like this. Index format. But you are in the current path? Yeah, but maybe that's a binary or something like that. No. Oh. I guess you can do instead of dot, you can do a regex, right? Like MD? Everything finished in MD? No. Let me start it again. Give me a command. To run in Linux, to replace app name. Is app in dash name, right? Dash name. In all files my current path. Okay. I think I did something to my Git. What? I haven't done much. I cloned the repository again because I think I killed something in Git itself. But do you have the specific visual code or any UI editor? Do you use Vim? I'm just using Vim. I think you can do it with Vim, right? No. It doesn't do anything. Why? I mean, there is this DevCTL command that should do this. Anyway. Which was it? I asked it again to the TabGPP and I had the same command. Well, it's not exactly the same. Yeah, no, it's the same. I'm not sure if it's the same or not. Yeah, I think it's the same. I think set dash I is not correct because set dash I doesn't replace. Great. It just outputs. No, I should be added in place on the file. Why doesn't it work? You see? If you replace the last character with plus, which is an addition. Why do you need a semicolon? To close the exit command. This is just closing this one. So it should be fine. Oh, now it worked. Let me see. Yeah, now it worked. Why did it work now? Okay, now I can do it. There is something I guess in the directory. Oh no, now I killed my vendor. I need to do this again. I don't have it here. So find, no, rep rep, find, and I just do it one by one. Maybe as main then circle CI config pull request template and I know that's a set shell so my set shell is complaining. Oh, come on. I can only expand it. I think that doesn't work. Maybe it works. Did it work? Yeah. So what else is left? Do the rep or? The helm folder you have to do it using mb command again. Yeah. This was wrong. No. So I think it's really fine. Circle CI has been outdated too. Okay. Yeah. I need to do the vent now. I need to do this one again. Where was it? Here. And then this one needs to be the URL from that string. Oops. Okay. Yes. Yes. Okay. Yes. Okay. That should be it. Oops. You can try also now to run vendor and see what happens. Do I need to install it? Yeah. You need to install vendor first because you have to run it and you have to also have docker or something compatible to run to pull the chart. Okay. So what else to come on? It's been the sync and that's it. We will look for the config and it will fetch the Okay. Now you can see if it's there. And now you can do is and depends dependency update to see it gets the dependencies. The vendor lock also needs to Okay. But I need to add everything to get right. Yeah. Vendor lock is fine. Yeah. Then in the helm directory you can do helm dependency update so you make sure it does the right. Here in the chart I can add the dependencies you mean? You can add also the values Did I do the wrong replacement here? We didn't remove the brackets, right? Yeah. Let me just quickly add the dependencies here No, yeah. Too bad. You can search and replace which said this is easier because then you have the brackets so you can look for brackets and replace it without. Okay. Yeah. Just earlier we said or right? Or you can not target and we said the entire directory I'm not sure. Yeah. No, I mean it's it's again all these files and I'm afraid to kill my git You can push it. You can push it your chance and then I'm just I'm just going through the files one by one so it's abs-circle ci github pull request template change So I think that looks good. Yeah. What? Oh, I need to create a branch. Yeah, because the FTL Yeah. Protect the branch, right? Yeah. Okay. Pull request Should I ping you? Yes, please. Okay. Okay. Okay. Okay. Github I have not yet requested I just review Yeah. Looking at the changes looks looks fine. Have you tried the dependency update to see if it works? Uh Why do I need to do this? Like, I know that this exact version works at the moment. Because I have installed it and I don't know if I want to change anything of that setup now. Yeah, I mean, I installed it on my machine and it worked fine. Okay. Cool. Then I guess you can follow the retagging part. Like, ideally what Honeywire wants or what we want is to always retag the tools that we want. So it will be a matter of checking the container image that n8n uses and we can potentially retag it. But I think after merging this we should see the app chart already in the catalog. Do we need to add it to Github? Yes, because it will probably update something that is not up to date in the app template. But there are no other files that need to be there for Github actions or anything like that? That's through Github? The Github repository? It's through Github, yeah. Indeed, Kabbage released a Github action called update chart which does the vendor automatically for you when you create a new branch which is called update chart. It pulls from the upstream and then it updates the chart and everything and pushes it as a PR. So it's easy for you to update. Let me check if I understand this correctly. Here it says the action is deployed to the repository Giants on Github and the desired repository is set. Again, install update chart to true and run synchronize Github action. So how do I do this? I think when you add the app in the Github repository file you have to put the install update chart variable to true. I can probably see this with the Envoy gateway. Yeah, and you can ping me if you want and we can double check. I create the repository and how do I run the synchronize Github action? Synchronize Github action? Ah, when you do the PR with the new one? In Github, the repository you mean? We have to release it or something like that. I always forget that part. I think there should be a release or something. Yeah, it should be a release and then it triggers all the changes. But it should be specified in the repository. Do I need to do some retagging here? Ideally, yes. You can also trigger the synchronize workflow and it will do the trick. Releasing a new version it will do that too. I have a one-on-one. I tried to figure it out otherwise I'm going to ping you and I don't know how much time I have today anyway. Thank you very much to get where I am. Maybe it's good to put these things together to have one guide that is up to date again, I guess. Because I think it's really crucial for us if we want to test out things that those kind of steps are almost automated when we can just point something to a Helm chart and then stuff happens. That would be really, really great. But it was not painful. It was finding out some things but if we have a guide I think with Vendia and everything it's kind of easy to do. I think as soon as we release a version of your app it should be almost everything done. Within an hour you can have it working. What we can do also to test is just take a chart from AppStream, the one you selected and then say, Devin, can you do that for me and let's see if I can do all the changes or something like that. But yeah, updating this document will be also the thing to do. Let me work on that too. Thank you very much. Bye.  

# devctl
The tool called DeepCTL in the transcript is actually called devctl and this is the repo setup command:

Configure GitHub repository with:

 - Settings
 - Permissions
 - Default branch protection rules

Usage:
  devctl repo setup [flags] REPOSITORY
  devctl repo setup [command]

Available Commands:
  ci-webhooks Configure GitHub repository
  renovate    Enable (or disable) Renovate for the repository

Flags:
      --allow-automerge              Allow auto-merge on pull requests, or false to forbid it. (default true)
      --allow-mergecommit            Allow merging pull requests with a merge commit, or false to prevent it.
      --allow-rebasemerge            Allow rebase-merging pull requests, or false to prevent it.
      --allow-squashmerge            Allow squash-merging pull requests, or false to prevent it. (default true)
      --allow-updatebranch           Whenever there are new changes available in the base branch, present an “update branch” option in the pull request. (default true)
      --archived                     Mark this repo as archived.
      --checks strings               Check context names for branch protection. Default will add all auto-detected checks, this can be disabled by passing an empty string. Overrides "--checks-filter"
      --checks-filter string         Provide a regex to filter checks. Checks matching the regex will be ignored. Empty string disables filter (all checks are accepted). (default "aliyun")
      --default-branch string        Default branch name (default "main")
      --delete-branch-on-merge       Automatically delete head branches when PRs are merged, or false to prevent it. (default true)
      --disable-branch-protection    Disable default branch protection
      --dry-run                      Dry-run or ready-only mode. Show what is being made but do not apply any change.
      --enable-issues                Enable issues for this repo, or false to remove them. (default true)
      --enable-projects              Enable projects for this repo, or false to remove them.
      --enable-wiki                  Enable wiki for this repo, false to remove it.
      --github-token-envvar string   Environment variable name for Github token. (default "GITHUB_TOKEN")
  -h, --help                         help for setup
      --permissions stringToString   Grant access to this repository using github_team_name=permission format. Multiple values can be provided as a comma separated list or using this flag multiple times. Permission can be one of: pull, push, admin, maintain, triage. (default [Employees=admin,bots=push])
      --renovate                     Sets up renovate for the repo (default true)

Global Flags:
      --log-level string   logging level (default "info")

Use "devctl repo setup [command] --help" for more information about a command.


# Attachments: Git logs  
The attached files provide more details about how to setup an app repository and configure the gitops repository to deploy that application on a cluster. Especially the small changes that had to be made to get around security and compliance policies to get CI and kyverno on the cluster happy.  Please analyze all the commits. 

# Feedback on slack
In preparation for the hackathon I'd like to understand better who is using vendir for keeping charts up-to-date. Would this become our prefered way to manage upstream charts? What other ways are teams using? What is the recommendation from @honeybadger /cc 
@piontec
Here is a first draft of what I'd like to do (probably needs some refinement but was generated from my creation of the n8n app recently): https://github.com/giantswarm/giantswarm/issues/32941

#32941 Easy and automated deployment and upgrades of apps
## Problem Statement
Currently, adding a new application (that already has a Helm chart) to a Giant Swarm Workload Cluster involves a significant number of manual steps. As evidenced by the provided transcript and git logs, the process requires developers to:
1. Manually create a repository from the app-template.
2. Configure vendir to fetch the upstream Helm chart.
3. Run devctl repo setup with correct parameters and authentication.
4. Manually (or via potentially platform-dependent scripts like sed) replace placeholder values across multiple files.
5. Manually reconcile dependencies listed in the upstream Chart.yaml with our own helm/app-name/Chart.yaml.
Show more
Assignees
@teemow
Labels
hackathon, team/planeteers
<https://github.com/giantswarm/giantswarm|giantswarm/giantswarm>giantswarm/giantswarm | Today at 08:39 | Added by GitHub
25 replies

Antonia
  Today at 08:48
We use vendir in rocket for a few of our charts. So far it's been working well but we still need some patches to inject things like our team label.
Mati
:no_entry:  Today at 08:48
in Cabbage we use vendir + a wrapper script that handles patches
Quentin
  Today at 08:48
Atlas uses helm chart dependencies. It has drawbacks that we cannot fix upstream without actually fixing the upstream chart so it's a bit slower sure but then we don't have to care about vendir at all (edited) 
Mati
:no_entry:  Today at 08:50
we used to do vendir + upstream-repo-clone but that's not longer the case. with the sync.sh wrapper script we have a series for patches that we apply after pulling from upstream directly with vendir (edited) 
Laszlo Uveges
:elephant:  Today at 09:10
My 5 cent: in Honeybadger, sometimes we use manual - e.g. flux, cos they release a single yaml file upstream with all manifests, but with KO out once, we could potentially just use flux operator maybe -, we also use a git subtree based auto update mechanism - thats a little tricky and complex from what I know about it, but its super nice when you just get a pr with the changes and might just need to solve some conflicts.
We dont use vendir, tried at a couple of projects back then and it did not cut it. Not played with chart depenencies much, we want to rather customize and extend the charts.
teemow
:sonic:  Today at 09:15
@Mati
 do you have a link to the script :pray::skin-tone-2:
Mati
:no_entry:  Today at 09:15
https://github.com/giantswarm/cilium-app/tree/main/sync
:heart-8bit-1:
Marco
  Today at 09:19
The upstream repo is still useful for filing PRs, I think. (edited) 
Mati
:no_entry:  Today at 09:20
yes. exaclty. we keep the upstream repo around for contributions
Marco
  Today at 09:20
So everything you proposed there and already want to use in the Giant Swarm app is also a patch in the app repo?
09:21
Also, do not get me wrong. Didn't want to sound like "you need the upstream repo, y u no?!". :smile:
IIRC some other teams are using the upstream repo fork to build custom images in case we have more than just changes to the chart.
piontec
  Today at 09:31
adding to what Laszlo said: the script that tries to solve an update without leaving git is used for example here: https://github.com/giantswarm/zot/blob/main/.github/workflows/auto-upgrade.yaml
auto-upgrade.yaml
name: Auto upgrade the chart from upstream
on:
  schedule:
    - cron: "07 13 * * *"
Show more
<https://github.com/giantswarm/zot|giantswarm/zot>giantswarm/zot | Added by GitHub
09:34
We tried vendir and found it cumbersome, a lot of manual merging that otherwise can be handled by git, as both sources come from the same source repo
Pau
  Today at 09:36
at phoenix we use vendir + kustomize in some cases to generate the chart
09:39
vendir has the problem that you can't merge files from different folders (even within the same repo)
Quentin
  Today at 09:41
It's also really painful to use and maintain when you have to maintain multiple release branches right? That was my main beef with it
Pau
  Today at 09:42
we have not faced that yet
09:42
it's easy: don't do breaking changes :troll_parrot:
Mati
:no_entry:  Today at 09:42
we do multiple release branches without problems
Pau
  Today at 09:44
https://github.com/giantswarm/karpenter-app/blob/update-vendir/vendir.yml
https://github.com/giantswarm/karpenter-app/blob/f7bf2055ade21a70009ff9cde304751b20753f26/Makefile#L27
here is an example of vendir + kustomize where we merge resources from 2 folders and also add annotations/labels that can't be added upstream
vendir.yml
<https://github.com/giantswarm/karpenter-app|giantswarm/karpenter-app>giantswarm/karpenter-app | Added by GitHub

Makefile
<https://github.com/giantswarm/karpenter-app|giantswarm/karpenter-app>giantswarm/karpenter-app | Added by GitHub
Quentin
  Today at 09:45
Well I've done it with keda and keda supports only 3 kubernetes versions so you need to keep it up to date a lot and we have/had a lot of cluster versions :D
Pau
  Today at 09:45
yeah... the patch releases of old major are always a pain
Quentin
  Today at 09:49
Can you not do some upstream contrib so those labels are added from values?
Pau
  Today at 09:51
in some cases, the CRDs are not part of the helm chart even
Quentin
  Today at 09:59
true

# Another transcript of Mati explaining the sync.sh script from Team Cabbage
Matías Charriere: So, let me try to share the screen. So, what we do there is we have this wrapper script where we call bender to pull the sources from the app and then apply a bunch of patches on top of that. the budgets are usually things that we cannot contribute to upstream because I don't know we add something super specific or upstream is not in favor of doing that.
Timo Derstappen: Our team labels or…
Timo Derstappen: stuff like that, right? Yeah.
Matías Charriere: Yeah, with the label is we try to use the common labels thing and that's a change that they upstream usually is willing to accept in any project adding a label it's adding a way to add labels it's okay in general but there are things like the values yes…
Timo Derstappen: But…
Timo Derstappen: but you kind of need to add it to the defaults of the value, right? Yeah.
Fernando Ripoll: the Bersian.
Matías Charriere: 
Matías Charriere: but the values is always something that we change,…
Timo Derstappen: Yeah. But…
Matías Charriere: it's not something that we leave.
Timo Derstappen: but the values is something that is hard to merge, right?
Matías Charriere: So depending on the project things are little different. So that's the other problem we currently have with this method is that we don't have a centralized way to deploy the script. So we change the script depending on the project. let me see if I can share my screen because that's going to be yeah I please you can see your map scrape or…
Fernando Ripoll: I can start the screen with a script if you want. I think is here in the thread you pointed to.
Matías Charriere: It's okay. we have spreading different apps. Okay. Yeah,…
Fernando Ripoll: Where are you here? Right.
Matías Charriere: that one. Yeah, So, here we have The script is fairly simple.  So we do a first stage between line 10 and 14 that we basically do a vendor sync and then a dependency update that's depending on the app also because some apps have dependencies and some apps don't have held dependencies right so we do that just to have everything clean and then we apply the patches each patch it's a
Matías Charriere: different case. Let's say some patches are just g patches where we apply the patch and…
Matías Charriere: it's a real g patch and some more patches are adding files for example or replacing files that depends on each patch. these values for example is a bit more complex.
Fernando Ripoll: For example,…
Fernando Ripoll: we can look at one, right?
Matías Charriere: We do a bunch of set you can see and we replace the file with our own values we intend to change this but yeah this is how it is now.
Fernando Ripoll: Mhm.
Matías Charriere: And we do a bunch of stuff because we are pulling ben selium from different sources from the g repo and…
Timo Derstappen: Did you take a look at the sub tree command that Bjontek did? And
Matías Charriere: from the official hem chart and then merge together that together to compose the final output. yeah yeah yeah yeah yeah we used to use sub tree and we had problems mostly on the merge strategies because some of our changes were breaking changes for upstream.
Matías Charriere: So, every time we had up an upgrade, we had issues and it's harder to track with changes because we always use commit squash merges and when you do a squash of a sub tree you lose all the information and everything is tracked inside the messages because sub tree works like that.  So sub tree will track what is doing based on the commit messages and that can be a problem. If you do a squash then you need to remember not to do a squash for certain PRs. So that was our main issue with sub tree.
00:05:00
Matías Charriere: we changed from sub tree to bender using the upstream repo which is what some teams are doing like I think it's shield is doing that but then we decided to stop using the upstream fork and then introduced this script that basically keeps everything inside the repo and from my experience it's easier to keep track of the changes and even getting rid of those changes because now I let's say that we drop the network policies patch because Art stream has it. It's just removing the folder and that's it.
Matías Charriere: you get a clean helm repo from absent. what we are also doing in this sync script is that we store the differences between what we do and upstream. This is to keep track of the changes in case we make a change that is not supposed to happen.
Matías Charriere: And it's easier for review for example because you get to guess get used to doing a review of a div of a deep that's a bit tricky…
Fernando Ripoll: Mhm.  So cool.
Matías Charriere: but once you get the idea of how it works is like you can see right away what changes were introduced by upstream and that you weren't supposed to be changing let's  So basically that's the whole thing. I'm not trying to sell this. I think that work for us for our team and for our repos. some teams are doing different things like building images on the upstream repo. We don't do that. We always use the upstream images for example.
Matías Charriere: So this is mostly for syncing H releases sorry H charts or teams are doing things differently because sometimes it's what they need to do right so  Yeah. Yeah.
Timo Derstappen: Yeah, I would like to come up with a tool that kind of abstracts this for all the applications.
Timo Derstappen: So the applications can grow and you can have the patches and you can clean up the patches or you can add patches easily. and then yeah I mean it's specific per app and we might need to find a way to make the patches easier than using set commands or something like that to have better usability.
Timo Derstappen: But I agree that having this visible is super helpful because exactly this is something that is custom to giant one that we are adding here and do you can even put the reason in there…
Fernando Ripoll: Have you considered the option to use customize at that point or
Timo Derstappen: why is this there and stuff like that. So it's better than messing with git trees all the time and not knowing what is where and when and why. Yeah.
Matías Charriere: Yeah. Yeah.
Matías Charriere: Yeah. Yeah. we agree with I totally agree with that. I know that our team is also in line with the problem we see with customize is that you cannot template on top of that.
Timo Derstappen: Come on.
Matías Charriere: So the hem chart is a bit different of what you expect in a hem chart. I'm not sure what the case for customize is for files that are not templated in Helm. That's fine. But as soon as you need to do I don't know change the name of the deployment is going to break or…
00:10:00
Matías Charriere: I'm not sure what other teams are doing with customer. what I saw is that you get an specific chart for your application…
Fernando Ripoll: I think that Yeah,…
Matías Charriere: but that's it. you cannot customize a lot
Fernando Ripoll: I'm not sure, but I thought that you could potentially change the template adding, for example, extra here or changing the name of a field.
Fernando Ripoll: I'm not completely sure but when I was looking at the I think it's CSI EBS chart they were using to modify some of the chart templates…
Fernando Ripoll: but yeah I didn't try Mhm.
Matías Charriere: Yeah. Yeah.
Matías Charriere: No, no, I didn't really look deep into it. I remember there was flax. I think that they were using customize at some point, but I haven't really follow it. All right,…
Fernando Ripoll: Okay. Okay.
Matías Charriere: I will jump out and…
Timo Derstappen: Yeah, we can stop it.
Matías Charriere: join my Hagaton project. let me know if you have any question.
Matías Charriere: Maybe I join tomorrow again if I finish early. Okay, bye.

# The sync.sh script
#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd ) ; readonly dir
cd "${dir}/.."

# Stage 1 sync - intermediate to the ./vendir folder
set -x
vendir sync
helm dependency update helm/cilium/
{ set +x; } 2>/dev/null

# Patches
./sync/patches/eni/patch.sh
./sync/patches/image_registries/patch.sh
./sync/patches/readme/patch.sh
./sync/patches/networkpolicies/patch.sh
./sync/patches/values/patch.sh

# Store diffs
rm -f ./diffs/*
for f in $(git --no-pager diff --no-exit-code --no-color --no-index vendor/cilium/install/kubernetes helm --name-only) ; do
        [[ "$f" == "helm/cilium/Chart.yaml" ]] && continue
        [[ "$f" == "helm/cilium/Chart.lock" ]] && continue
        [[ "$f" == "helm/cilium/README.md" ]] && continue
        [[ "$f" == "helm/cilium/values.schema.json" ]] && continue
        [[ "$f" == "helm/cilium/values.yaml" ]] && continue
        [[ "$f" =~ ^helm/cilium/charts/.* ]] && continue

        base_file="vendor/cilium/install/kubernetes/${f#"helm/"}"
        [[ ! -e $base_file ]] && base_file="vendor/cilium/${f#"helm/"}"
        [[ ! -e $base_file ]] && base_file="/dev/null"

        set +e
        set -x
        git --no-pager diff --no-exit-code --no-color --no-index "$base_file" "${f}" \
                > "./diffs/${f//\//__}.patch" # ${f//\//__} replaces all "/" with "__"

        { set +x; } 2>/dev/null
        set -e
        ret=$?
        if [ $ret -ne 0 ] && [ $ret -ne 1 ] ; then
                exit $ret
        fi
done

## How were patches generated?

First, stage the changes (in `./helm`) and the run:

> [!TIP]
> Skip the `-R` flags if the changes were added.

```bash
git --no-pager diff -R helm/cilium/templates/cilium-agent/daemonset.yaml \
        > sync/patches/eni/cilium_agent__daemonset.yaml.patch
git --no-pager diff -R helm/cilium/templates/cilium-configmap.yaml \
        > sync/patches/eni/cilium-configmap.yaml.patch
```

## What is the patched change?

In case something goes wrong this is the raw change:


In file `./helm/cilium/templates/cilium-agent/daemonset.yaml` add the env vars below to `cilium-agent` and `config` containers:

```
        - name: CILIUM_CNI_CHAINING_MODE
          valueFrom:
            configMapKeyRef:
              name: cilium-config
              key: cni-chaining-mode
              optional: true
        - name: CILIUM_CUSTOM_CNI_CONF
          valueFrom:
            configMapKeyRef:
              name: cilium-config
              key: custom-cni-conf
              optional: true
```


In file `./helm/cilium/templates/cilium-configmap.yaml` replace:

```
{{- if .Values.cni.customConf  }}
  # legacy: v1.13 and before needed cni.customConf: true with cni.configMap
  write-cni-conf-when-ready: {{ .Values.cni.hostConfDirMountPath }}/05-cilium.conflist
{{- end }}
```

with:

```
  write-cni-conf-when-ready: {{ .Values.cni.hostConfDirMountPath }}/21-cilium.conflist
```
# Patch script
#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

repo_dir=$(git rev-parse --show-toplevel) ; readonly repo_dir
script_dir=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd ) ; readonly script_dir

cd "${repo_dir}"

readonly script_dir_rel=".${script_dir#"${repo_dir}"}"

set -x
git apply "${script_dir_rel}/cilium_agent__daemonset.yaml.patch"
git apply "${script_dir_rel}/cilium-configmap.yaml.patch"
{ set +x; } 2>/dev/null


# Task  
I'd like to automate this as much as possbile. To make it easy for engineers to add new apps and keep them up to date. At best this should be almost hands-free. Please create an issue for my hackathon project. Format the issue in github markdown.