package ami

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakeObjectGetter struct {
	key  string
	body string
	err  error
}

func (f *fakeObjectGetter) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.key = *params.Key
	if f.err != nil {
		return nil, f.err
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader(f.body))}, nil
}

func Test_getFlatcarRelease(t *testing.T) {
	config := Config{Arch: "amd64-usr", Channel: "stable", ChinaBucketName: "bucket"}

	t.Run("found", func(t *testing.T) {
		client := &fakeObjectGetter{body: `{"amis":[{"name":"cn-north-1","hvm":"ami-123"}]}`}

		got, err := getFlatcarRelease(context.Background(), client, config, "1.2.3")
		if err != nil {
			t.Fatal(err)
		}
		if want := map[string]string{"cn-north-1": "ami-123"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		if want := "stable/amd64-usr/1.2.3.json"; client.key != want {
			t.Fatalf("got key %q, want %q", client.key, want)
		}
	})

	t.Run("missing key is not an error", func(t *testing.T) {
		client := &fakeObjectGetter{err: &types.NoSuchKey{}}

		got, err := getFlatcarRelease(context.Background(), client, config, "1.2.3")
		if err != nil || got != nil {
			t.Fatalf("got %v, %v; want nil, nil", got, err)
		}
	})

	t.Run("other errors are returned", func(t *testing.T) {
		client := &fakeObjectGetter{err: errors.New("access denied")}

		if _, err := getFlatcarRelease(context.Background(), client, config, "1.2.3"); err == nil {
			t.Fatal("expected an error")
		}
	})
}
