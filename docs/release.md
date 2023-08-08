# Creating vintage releases

The tool can be used to create legacy cluster releases, for example the ones stored in [giantswarm/releases](https://github.com/giantswarm/releases).

Example:

```nohighlight
devctl release create \
    --provider aws \
    --base 18.0.1 \
    --name 18.0.2 \
    --component aws-operator@13.2.1-dev \
    --overwrite
```
