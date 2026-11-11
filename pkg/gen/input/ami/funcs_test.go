package ami

import (
	"reflect"
	"strings"
	"testing"
)

func Test_scrapeVersions(t *testing.T) {
	testCases := []struct {
		name     string
		body     string
		expected []string
	}{
		{
			name: "case 1: flatcar style",
			body: `<html>
	<body>
		<main>
			<div class="listing">
				<table aria-describedby="summary">
					<tbody>
					<tr class="file">
						<td>
							<a href="./2345.3.1/">
								<svg width="1.5em" height="1em" version="1.1" viewBox="0 0 317 259"><use xlink:href="#folder"></use></svg>
								<span class="name">2345.3.1</span>
							</a>
						</td>
						<td data-order="-1">&mdash;</td>
						<td class="hideable"><time datetime="2020-04-01T09:27:01Z">04/01/2020 09:27:01 AM +00:00</time></td>
					</tr>
					<tr class="file">
						<td>
							<a href="./current/">
								<svg width="1.5em" height="1em" version="1.1" viewBox="0 0 317 259"><use xlink:href="#folder-shortcut"></use></svg>
								<span class="name">current</span>
							</a>
						</td>
						<td data-order="-1">&mdash;</td>
						<td class="hideable"><time datetime="2020-03-31T16:26:04Z">03/31/2020 04:26:04 PM +00:00</time></td>
					</tr>
					</tbody>
				</table>
			</div>
		</main>
	</body>
</html>
`,
			expected: []string{"2345.3.1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(tc.body)
			versions, err := scrapeVersions(reader)
			if err != nil {
				t.Fatal(err)
			}
			if len(versions) != len(tc.expected) {
				t.Fatalf("version count %d doesn't match expected %d", len(versions), len(tc.expected))
			}
			for i := range versions {
				if versions[i] != tc.expected[i] {
					t.Fatalf("version at index %d, %s, doesn't match expected %s", i, versions[i], tc.expected[i])
				}
			}
		})
	}
}

func Test_scrapeVersionAMIs(t *testing.T) {
	testCases := []struct {
		name     string
		body     string
		expected map[string]string
	}{
		{
			name: "case 1: flatcar style",
			body: `{
  "amis": [
    {
      "name": "ap-east-1",
      "hvm": "ami-0e28e38ecce552688"
    }
  ]
}`,
			expected: map[string]string{
				"ap-east-1": "ami-0e28e38ecce552688",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := strings.NewReader(tc.body)
			amis, err := scrapeVersionAMI(reader)
			if err != nil {
				t.Fatal(err)
			}
			if len(amis) != len(tc.expected) {
				t.Fatalf("version count %d doesn't match expected %d", len(amis), len(tc.expected))
			}
			for i := range amis {
				if amis[i] != tc.expected[i] {
					t.Fatalf("ami at key %s, %s, doesn't match expected %s", i, amis[i], tc.expected[i])
				}
			}
		})
	}
}

func Test_withChinaAMIs(t *testing.T) {
	t.Run("china AMIs are added", func(t *testing.T) {
		got, ok := withChinaAMIs(map[string]string{"eu-west-1": "ami-1"}, map[string]string{"cn-north-1": "ami-2"})
		if want := map[string]string{"eu-west-1": "ami-1", "cn-north-1": "ami-2"}; !ok || !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, %v; want %v, true", got, ok, want)
		}
	})

	t.Run("a release without china AMIs is left out", func(t *testing.T) {
		if got, ok := withChinaAMIs(map[string]string{"eu-west-1": "ami-1"}, nil); ok || got != nil {
			t.Fatalf("got %v, %v; want nil, false", got, ok)
		}
	})
}
