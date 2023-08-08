# Using the `gen` commands to generate files

The `gen` command family is designed to create common files in repositories, adapted specifically for the repository [flavour](flavours.md) and/or programming language used.

Files are written to the current directory. The assumption is that the current working directory is the root directory of a cloned repository.

Usually these commands are executed via automation in the [giantswarm/github](https://github.com/giantswarm/github/actions/workflows/synchronize.yaml) repository, but this can also be done manually/locally.

Note: the added files are not meant for later editing, as changes would be overwritten by a subsequent `devctl` execution.

## Generating workflow files

Creates common GitHub actions workflows (for CI/CD) in the `.github/workflows` directory.

Example:

```nohighlight
devctl gen workflows --flavour cli
```

## Generating Makefiles

Creates common `Makefile` and includes in the root directory.

Example:

```nohighlight
devctl gen workflows --flavour cli --language go
```

## Generating renovate configuration

Generates a `renovate.json` file in the repo root to configure [renovate](https://docs.renovatebot.com/), which automatically updates dependencies in the configured repository.

```nohighlight
devctl gen renovate --language LANGUAGE
```

Note: The `LANGUAGE` value is not validated currently. From code, as of writing this docs, `go` and `python` were the only values checked for. (Usability improvement welcome!)
