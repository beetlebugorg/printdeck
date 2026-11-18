package api

import (
	"context"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
)

// Route53DNS is the DNS of the names of the decks: the records of the hosted zone Zone. The role
// of the function can change only the names under .d.<domain>, and only the types A, AAAA and
// TXT (deploy/template.yaml).
type Route53DNS struct {
	Client *route53.Client
	Zone   string
}

// changeWait is the most time that Set waits for a change to reach the name servers.
const changeWait = 2 * time.Minute

func (r *Route53DNS) Set(ctx context.Context, name, typ string, values []string, ttl int64, wait bool) error {
	recs := make([]types.ResourceRecord, len(values))
	for i, v := range values {
		recs[i] = types.ResourceRecord{Value: aws.String(v)}
	}
	out, err := r.Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(r.Zone),
		ChangeBatch: &types.ChangeBatch{Changes: []types.Change{{
			Action: types.ChangeActionUpsert,
			ResourceRecordSet: &types.ResourceRecordSet{
				Name: aws.String(name), Type: types.RRType(typ), TTL: aws.Int64(ttl), ResourceRecords: recs,
			},
		}}},
	})
	if err != nil || !wait {
		return err
	}
	w := route53.NewResourceRecordSetsChangedWaiter(r.Client, func(o *route53.ResourceRecordSetsChangedWaiterOptions) {
		o.MinDelay, o.MaxDelay = 2*time.Second, 5*time.Second
	})
	return w.Wait(ctx, &route53.GetChangeInput{Id: out.ChangeInfo.Id}, changeWait)
}

func (r *Route53DNS) Remove(ctx context.Context, name, typ string) error {
	// A delete must have the record as it is: read it first.
	out, err := r.Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId: aws.String(r.Zone), StartRecordName: aws.String(name), StartRecordType: types.RRType(typ), MaxItems: aws.Int32(1),
	})
	if err != nil {
		return err
	}
	if len(out.ResourceRecordSets) == 0 {
		return nil
	}
	set := out.ResourceRecordSets[0]
	if !strings.EqualFold(strings.TrimSuffix(aws.ToString(set.Name), "."), strings.TrimSuffix(name, ".")) || string(set.Type) != typ {
		return nil
	}
	_, err = r.Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(r.Zone),
		ChangeBatch:  &types.ChangeBatch{Changes: []types.Change{{Action: types.ChangeActionDelete, ResourceRecordSet: &set}}},
	})
	return err
}
