package tools

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
)

//DownloadFromS3 will get the file from the given bucket and path and will return the complete path of downloaded file
//or an error
func DownloadFromS3(bucketName string, itemName string) (string, error) {

	sess, err := session.NewSession(&aws.Config{Region: aws.String("eu-west-3")})
	if err != nil {
		return "", err
	}

	extension := filepath.Ext(itemName)

	filename := "/tmp/" + RandomHex(30) + extension
	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}

	defer file.Close()

	downloader := s3manager.NewDownloader(sess)
	_, err = downloader.Download(file,
		&s3.GetObjectInput{
			Bucket: aws.String(bucketName),
			Key:    aws.String(itemName),
		})
	if err != nil {
		return "", err
	}
	return filename, nil
}

/*GetContentFromS3 will download the conf file from S3 and return the content as a file*/
func GetContentFromS3(bucket string, bucketFileConf string) ([]byte, error) {
	//download file from S3
	filename, err := DownloadFromS3(bucket, bucketFileConf)
	if err != nil {
		return nil, err
	}
	defer os.Remove(filename)
	return ioutil.ReadFile(filename)
}

//ListObjects returns a list of objects in a bucket name
func ListObjects(bucketName string) ([]string, error) {
	sess, err := session.NewSession(&aws.Config{Region: aws.String("eu-west-3")})
	if err != nil {
		return nil, err
	}

	result := []string{}
	svc := s3.New(sess)
	err = svc.ListObjectsPages(&s3.ListObjectsInput{
		Bucket: &bucketName,
	}, func(p *s3.ListObjectsOutput, last bool) (shouldContinue bool) {
		for _, obj := range p.Contents {
			result = append(result, *obj.Key)
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
