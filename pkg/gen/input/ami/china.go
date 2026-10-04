package ami

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/giantswarm/microerror"
)

func getChinaFlatcarRelease(config Config, version string) (map[string]string, error) {
	ctx := context.Background()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(config.ChinaBucketRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.ChinaAWSAccessKeyID, config.ChinaAWSSecretAccessKey, "")),
	)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return getFlatcarRelease(ctx, s3.NewFromConfig(cfg), config, version)
}

type objectGetter interface {
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

func getFlatcarRelease(ctx context.Context, client objectGetter, config Config, version string) (map[string]string, error) {
	result, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(config.ChinaBucketName),
		Key:    aws.String(fmt.Sprintf("%s/%s/%s.json", config.Channel, config.Arch, version)),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			// Not found, but that's fine
			fmt.Printf("Release %s not found in china\n", version)
			return nil, nil
		}
		return nil, microerror.Mask(err)
	}
	defer result.Body.Close()

	chinaVersionAMI, err := scrapeVersionAMI(result.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return chinaVersionAMI, nil
}
