package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/urfave/cli/v2"
	"log"
	"net"
	"os"
	"time"
)

const (
	appName = ""
	appDesc = ""
)

var (
	revision string

	globalArguments = []cli.Flag{
		&cli.StringFlag{
			Name:     "aws-access-key",
			EnvVars:  []string{"AWS_ACCESS_KEY"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "aws-secret-key",
			EnvVars:  []string{"AWS_SECRET_KEY"},
			Required: true,
		},
		&cli.StringFlag{
			Name:     "aws-region",
			EnvVars:  []string{"AWS_REGION"},
			Required: true,
		},
	}

	aggregateAndSyncArgs = []cli.Flag{
		&cli.StringFlag{
			Name:     "hosted-zone",
			Required: true,
		},
		&cli.StringSliceFlag{
			Name:     "source-record",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "target-record",
			Required: true,
		},
		&cli.Int64Flag{
			Name:     "poll-interval-seconds",
			Required: true,
		},
	}
)

func main() {
	app := cli.NewApp()
	app.Name = appName
	app.Description = appDesc
	app.Version = revision
	app.Flags = globalArguments
	app.Commands = []*cli.Command{
		{
			Name:   "aggregate-and-sync",
			Flags:  aggregateAndSyncArgs,
			Action: aggregateAndSync,
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Panic(err)
	}
}

func aggregateAndSync(c *cli.Context) error {
	newSession, err := session.NewSession(
		&aws.Config{
			Region:      aws.String(c.String("aws-region")),
			Credentials: credentials.NewStaticCredentials(c.String("aws-access-key"), c.String("aws-secret-key"), ""),
		},
	)
	if err != nil {
		panic(err)
	}

	r53 := route53.New(newSession)

	for {
		select {
		case <-time.After(time.Second * time.Duration(c.Int("poll-interval-seconds"))):
			// resolve target
			targetIPs, err := net.LookupIP(c.String("target-record"))
			if err != nil {
				panic(err)
			}

			// resolve sources
			var sourceIPs []net.IP
			for _, sourceRecord := range c.StringSlice("source-record") {
				sourceIP, err := net.LookupIP(sourceRecord)
				if err != nil {
					panic(err)
				}

				sourceIPs = append(sourceIPs, sourceIP...)
			}

			// verify target contains up to date sources
			targetStale := false
			for _, sourceIP := range sourceIPs {
				exists := false

				for _, targetIP := range targetIPs {
					if targetIP.Equal(sourceIP) {
						exists = true
					}
				}

				if exists == false {
					targetStale = true
				}
			}

			// update target
			if targetStale {
				println("target stale updating")

				// get record
				params := &route53.ListResourceRecordSetsInput{
					HostedZoneId:    aws.String(c.String("hosted-zone")),
					MaxItems:        aws.String("1"),
					StartRecordName: aws.String(c.String("target-record")),
					StartRecordType: aws.String(route53.RRTypeA),
				}

				resp, err := r53.ListResourceRecordSets(params)
				if err != nil {
					panic(err)
				}

				recordSet := resp.ResourceRecordSets[0]

				// mutate record
				var records []*route53.ResourceRecord
				for _, sourceIP := range sourceIPs {
					records = append(records, &route53.ResourceRecord{
						Value: aws.String(sourceIP.String()),
					})
				}

				recordSet.ResourceRecords = records

				//update record
				input := &route53.ChangeResourceRecordSetsInput{
					ChangeBatch: &route53.ChangeBatch{
						Changes: []*route53.Change{
							{
								Action:            aws.String(route53.ChangeActionUpsert),
								ResourceRecordSet: recordSet,
							},
						},
					},
					HostedZoneId: aws.String(c.String("hosted-zone")),
				}

				_, err = r53.ChangeResourceRecordSets(input)
				if err != nil {
					panic(err)
				}
			} else {
				println("target up to date")
			}

		case <-c.Done():
			return nil
		}
	}

	return nil
}
