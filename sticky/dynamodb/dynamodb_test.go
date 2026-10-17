package dynamodb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/avsbhq/avsb-go/core"
)

// Behaviour tests against a recording fake of the two item calls the service
// makes. The fake keeps items by (pk, sk) the way the table's composite key
// does, records every request, and fails on demand.

const table = "avsb_sticky"

var treatment = core.StickyAssignment{
	VariationID: "var_treatment",
	RuleID:      "rule_ab_1",
	RuleType:    "ab_test",
	AssignedAt:  1760000000000,
}

type fakeItems struct {
	items  map[[2]string]map[string]types.AttributeValue
	gets   []*dynamodb.GetItemInput
	puts   []*dynamodb.PutItemInput
	getErr error
	putErr error
}

func newFake() *fakeItems {
	return &fakeItems{items: map[[2]string]map[string]types.AttributeValue{}}
}

func stringAttr(t *testing.T, item map[string]types.AttributeValue, name string) string {
	t.Helper()
	v, ok := item[name].(*types.AttributeValueMemberS)
	if !ok {
		t.Fatalf("attribute %q: want a string attribute, got %T", name, item[name])
	}
	return v.Value
}

func keyOf(item map[string]types.AttributeValue) [2]string {
	pk, _ := item["pk"].(*types.AttributeValueMemberS)
	sk, _ := item["sk"].(*types.AttributeValueMemberS)
	if pk == nil || sk == nil {
		return [2]string{}
	}
	return [2]string{pk.Value, sk.Value}
}

func (f *fakeItems) GetItem(_ context.Context, in *dynamodb.GetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error) {
	f.gets = append(f.gets, in)
	if f.getErr != nil {
		return nil, f.getErr
	}
	item, ok := f.items[keyOf(in.Key)]
	if !ok {
		return &dynamodb.GetItemOutput{}, nil
	}
	return &dynamodb.GetItemOutput{Item: item}, nil
}

func (f *fakeItems) PutItem(_ context.Context, in *dynamodb.PutItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error) {
	f.puts = append(f.puts, in)
	if f.putErr != nil {
		return nil, f.putErr
	}
	f.items[keyOf(in.Item)] = in.Item
	return &dynamodb.PutItemOutput{}, nil
}

func newService(fake *fakeItems, logger *slog.Logger) *DynamoDBService {
	return &DynamoDBService{client: fake, tableName: table, logger: logger}
}

func TestLookupMissReturnsNilAndAsksForTheCompositeKey(t *testing.T) {
	fake := newFake()
	svc := newService(fake, nil)

	if got := svc.Lookup("user_1", "checkout-v2"); got != nil {
		t.Fatalf("a miss is nil, not a zero assignment; got %+v", got)
	}
	if len(fake.gets) != 1 {
		t.Fatalf("want one GetItem, got %d", len(fake.gets))
	}
	in := fake.gets[0]
	if aws.ToString(in.TableName) != table {
		t.Errorf("table: want %q, got %q", table, aws.ToString(in.TableName))
	}
	if len(in.Key) != 2 || stringAttr(t, in.Key, "pk") != "user_1" || stringAttr(t, in.Key, "sk") != "checkout-v2" {
		t.Errorf("key: want pk=user_1, sk=checkout-v2 and nothing else, got %v", in.Key)
	}
}

func TestSaveThenLookupRoundTripsAndWritesOneItem(t *testing.T) {
	fake := newFake()
	svc := newService(fake, nil)

	svc.Save("user_1", "checkout-v2", treatment)

	if len(fake.puts) != 1 {
		t.Fatalf("want one PutItem, got %d", len(fake.puts))
	}
	put := fake.puts[0]
	if aws.ToString(put.TableName) != table {
		t.Errorf("table: want %q, got %q", table, aws.ToString(put.TableName))
	}
	if len(put.Item) != 3 || stringAttr(t, put.Item, "pk") != "user_1" || stringAttr(t, put.Item, "sk") != "checkout-v2" {
		t.Errorf("item: want pk, sk and assignment only, got %v", put.Item)
	}
	var stored core.StickyAssignment
	if err := json.Unmarshal([]byte(stringAttr(t, put.Item, "assignment")), &stored); err != nil {
		t.Fatalf("the assignment attribute is not the assignment's JSON: %v", err)
	}
	if stored != treatment {
		t.Errorf("stored record: want %+v, got %+v", treatment, stored)
	}

	got := svc.Lookup("user_1", "checkout-v2")
	if got == nil || *got != treatment {
		t.Fatalf("round trip: want %+v, got %+v", treatment, got)
	}
	if other := svc.Lookup("user_1", "pricing-page"); other != nil {
		t.Errorf("a different flag must not see this assignment; got %+v", other)
	}
}

func TestAnUnreadableItemIsAMiss(t *testing.T) {
	cases := map[string]map[string]types.AttributeValue{
		"no assignment attribute": {},
		"assignment is a number":  {"assignment": &types.AttributeValueMemberN{Value: "1"}},
		"assignment is not JSON":  {"assignment": &types.AttributeValueMemberS{Value: "{not json"}},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			fake := newFake()
			item := map[string]types.AttributeValue{
				"pk": &types.AttributeValueMemberS{Value: "u1"},
				"sk": &types.AttributeValueMemberS{Value: "flag-x"},
			}
			for k, v := range extra {
				item[k] = v
			}
			fake.items[[2]string{"u1", "flag-x"}] = item

			if got := newService(fake, nil).Lookup("u1", "flag-x"); got != nil {
				t.Errorf("want a miss, got %+v", got)
			}
		})
	}
}

func TestAFailedReadIsAMiss(t *testing.T) {
	fake := newFake()
	fake.items[[2]string{"u1", "flag-x"}] = map[string]types.AttributeValue{
		"assignment": &types.AttributeValueMemberS{Value: `{"variationId":"var_treatment"}`},
	}
	fake.getErr = errors.New("ProvisionedThroughputExceededException")

	if got := newService(fake, nil).Lookup("u1", "flag-x"); got != nil {
		t.Errorf("a failed read is a miss; got %+v", got)
	}
}

func TestAFailedWriteIsALoggedWarningAndNeverPanics(t *testing.T) {
	var logs bytes.Buffer
	fake := newFake()
	fake.putErr = errors.New("ResourceNotFoundException: no such table")

	newService(fake, slog.New(slog.NewTextHandler(&logs, nil))).Save("u1", "flag-x", treatment)

	out := logs.String()
	if !strings.Contains(out, "level=WARN") ||
		!strings.Contains(out, "avsb/sticky: dynamodb save failed for u1/flag-x") ||
		!strings.Contains(out, "no such table") {
		t.Errorf("a failed write must reach the logger as a warning with the cause; got %q", out)
	}

	// Without a logger the failure is dropped quietly: no panic, no write.
	newService(fake, nil).Save("u1", "flag-x", treatment)
	if len(fake.items) != 0 {
		t.Errorf("a failed write stores nothing; got %v", fake.items)
	}
}
