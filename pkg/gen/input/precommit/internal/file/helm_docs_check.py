import re
import sys
import yaml
# Match the description opener accepted by helm-values-schema-json v2.6.0,
# including an optional path and type. Only continuation lines lose text.
opener = re.compile(r'^#\s+(?:"[^"]*"|[^@\-\s]*)(?:\.(?:"[^"]*"|[^\s.]*))*\s*--')
annotations = {"@default", "@notationType", "@section"}
failed = False
for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as source:
        text = source.read()
    # A line beginning with '#' inside a YAML scalar is data, not a comment.
    # Scanning tokens also handles quoted multiline strings and block scalars.
    try:
        scalars = iter(token for token in yaml.scan(text) if isinstance(token, yaml.ScalarToken))
        scalar = next(scalars, None)
        offset = 0
        active = False
        for number, line in enumerate(text.splitlines(keepends=True), 1):
            comment = line.lstrip(" \t")
            start = offset + len(line) - len(comment)
            offset += len(line)
            while scalar is not None and scalar.end_mark.index <= start:
                scalar = next(scalars, None)
            if not comment.startswith("#") or (
                scalar is not None and scalar.start_mark.index <= start < scalar.end_mark.index
            ):
                active = False
                continue
            if not active:
                active = opener.match(comment) is not None
                continue
            annotation, separator, _ = comment[1:].strip().partition("--")
            if separator and annotation.strip() not in annotations:
                print(
                    f"{path}:{number}: '--' in a helm-docs description continuation is "
                    "silently dropped by helm-values-schema-json; reword this line "
                    "(for example, use an em dash).",
                    file=sys.stderr,
                )
                failed = True
    except yaml.YAMLError as error:
        print(f"{path}: {error}", file=sys.stderr)
        failed = True
sys.exit(1 if failed else 0)
