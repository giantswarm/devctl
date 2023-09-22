package ami

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/giantswarm/microerror"
)

func getChinaFlatcarRelease(config Config, version string) (map[string]string, error) {
	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(config.ChinaAWSAccessKeyID, config.ChinaAWSSecretAccessKey, ""),
		Region:      aws.String(config.ChinaBucketRegion),
	})
	if err != nil {
		return nil, microerror.Mask(err)
	}
	svc := s3.New(sess)
	input := &s3.GetObjectInput{
		Bucket: aws.String(config.ChinaBucketName),
		Key:    aws.String(fmt.Sprintf("%s/%s/%s.json", config.Channel, config.Arch, version)),
	}

	result, err := svc.GetObject(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchKey:
				// Not found, but that's fine
				fmt.Printf("Release %s not found in china\n", version)
				return nil, nil
			default:
				return nil, microerror.Mask(aerr)
			}
		}
		return nil, microerror.Mask(err)
	}

	chinaVersionAMI, err := scrapeVersionAMI(result.Body)
	if err != nil {
		return nil, microerror.Mask(err)
	}

	return chinaVersionAMI, nil
}
