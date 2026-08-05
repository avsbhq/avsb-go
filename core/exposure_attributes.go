package core

import "encoding/json"

// Exposure-attribute filtering.
//
// Port of packages/avsb-core/src/exposureAttributes.ts plus the context
// flattening in evaluate.ts. Exposure events carry only the attribute keys the
// project registered in the dashboard: unknown keys are dropped (a privacy and
// a ClickHouse-cardinality guard), and the ingestion worker drops them again
// server-side, so registering an attribute is what makes it queryable.
//
// Values are stringified because the destination column is a
// Map(String, String): objects and arrays keep their JSON text, scalars become
// their plain text.

// registeredAttributeKeys reads the datafile's registeredAttributes registry,
// accepting both shapes it has been published in: a bare string array, and an
// array of objects carrying a "key".
func registeredAttributeKeys(raw json.RawMessage) map[string]struct{} {
	arr, ok := jsonArray(raw)
	if !ok || len(arr) == 0 {
		return nil
	}
	keys := make(map[string]struct{}, len(arr))
	for _, entry := range arr {
		if s, ok := jsonString(entry); ok {
			keys[s] = struct{}{}
			continue
		}
		if obj, ok := jsonObject(entry); ok {
			if s, ok := jsonString(obj["key"]); ok {
				keys[s] = struct{}{}
			}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// BuildExposureAttributes flattens the context and filters it down to the
// project's registered attribute keys. Returns nil when nothing survives, so
// callers can omit the field entirely.
//
// Flattening matches the reference: the primary context contributes "userId",
// "name", and its own attribute keys unprefixed; every sub-context of a
// multi-context also contributes "<kind>.key", "<kind>.name", and
// "<kind>.<attribute>".
func BuildExposureAttributes(ctx EvalContext, df *Datafile) map[string]string {
	if df == nil {
		return nil
	}
	allowed := registeredAttributeKeys(df.RegAttributes)
	if allowed == nil {
		return nil
	}

	out := make(map[string]string)
	put := func(key string, raw json.RawMessage) {
		if _, ok := allowed[key]; !ok {
			return
		}
		if len(raw) == 0 || isJSONNull(raw) {
			return
		}
		out[key] = RawToString(raw)
	}

	if primary := PrimaryContext(ctx); primary != nil {
		put("userId", rawJSONString(primary.Key))
		if primary.Name != "" {
			put("name", rawJSONString(primary.Name))
		}
		for k, v := range primary.Attributes {
			put(k, v)
		}
	}

	if IsMultiContext(ctx) {
		for kind, raw := range ctx {
			if kind == "kind" {
				continue
			}
			sub := ParseSingle(raw)
			if sub == nil {
				continue
			}
			put(sub.Kind+".key", rawJSONString(sub.Key))
			if sub.Name != "" {
				put(sub.Kind+".name", rawJSONString(sub.Name))
			}
			for k, v := range sub.Attributes {
				put(sub.Kind+"."+k, v)
			}
		}
	}

	if len(out) == 0 {
		return nil
	}
	return out
}
