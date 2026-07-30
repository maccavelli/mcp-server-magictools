package db

import (
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// searchQueryTokens splits a query into alphanumeric tokens for keyword-field lookups.
func searchQueryTokens(queryStr string) []string {
	return strings.FieldsFunc(queryStr, func(c rune) bool {
		return (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9')
	})
}

// buildTokenizedKeywordQuery matches any single token against a keyword array field.
func buildTokenizedKeywordQuery(tokens []string, field string, boost float64) query.Query {
	bq := bleve.NewBooleanQuery()
	added := 0
	for _, token := range tokens {
		if len(token) < 2 {
			continue
		}
		tq := bleve.NewTermQuery(strings.ToLower(token))
		tq.SetField(field)
		bq.AddShould(tq)
		added++
	}
	if added == 0 {
		return bleve.NewMatchNoneQuery()
	}
	bq.SetMinShould(1)
	bq.SetBoost(boost)
	return bq
}

// buildFuzzyTokenMatchQuery creates a disjunction of fuzziness-scaled match queries.
func buildFuzzyTokenMatchQuery(queryStr string, tokens []string) query.Query {
	matchQuery := bleve.NewBooleanQuery()
	if len(tokens) == 0 {
		mq := bleve.NewMatchQuery(queryStr)
		mq.SetFuzziness(1)
		matchQuery.AddShould(mq)
	} else {
		for _, token := range tokens {
			mq := bleve.NewMatchQuery(token)
			switch {
			case len(token) <= 3:
				mq.SetFuzziness(0)
			case len(token) <= 6:
				mq.SetFuzziness(1)
			default:
				mq.SetFuzziness(2)
			}
			matchQuery.AddShould(mq)
		}
	}
	matchQuery.SetBoost(1.0)
	return matchQuery
}

type phraseFieldSpec struct {
	field string
	boost float64
}

// buildPhraseQueries adds MatchPhraseQuery legs for multi-word natural language intents.
func buildPhraseQueries(queryStr string, parts []string) []query.Query {
	if len(parts) < 2 {
		return nil
	}
	specs := []phraseFieldSpec{
		{"description", 2.5},
		{"intent", 3.5},
		{"synthetic_intents", 3.5},
		{"lite_summary", 2.5},
	}
	var queries []query.Query
	for _, spec := range specs {
		ph := bleve.NewMatchPhraseQuery(queryStr)
		ph.SetField(spec.field)
		ph.SetBoost(spec.boost)
		queries = append(queries, ph)
	}
	return queries
}

// buildDAGDiscoveryQueries wires indexed DAG metadata fields into the disjunction.
func buildDAGDiscoveryQueries(queryStr string) []query.Query {
	var queries []query.Query

	requiresQ := bleve.NewMatchQuery(queryStr)
	requiresQ.SetField("requires")
	requiresQ.SetBoost(2.0)
	queries = append(queries, requiresQ)

	triggersQ := bleve.NewMatchQuery(queryStr)
	triggersQ.SetField("triggers")
	triggersQ.SetBoost(2.0)
	queries = append(queries, triggersQ)

	roleQ := bleve.NewMatchQuery(queryStr)
	roleQ.SetField("role")
	roleQ.SetBoost(1.5)
	queries = append(queries, roleQ)

	return queries
}

// assembleCoreSearchQuery builds the primary disjunction with negative-trigger exclusion.
func assembleCoreSearchQuery(queryStr string) query.Query {
	if strings.TrimSpace(queryStr) == "" {
		return bleve.NewMatchAllQuery()
	}

	tokens := searchQueryTokens(queryStr)
	parts := strings.Fields(strings.TrimSpace(queryStr))

	exactUrnQuery := bleve.NewTermQuery(queryStr)
	exactUrnQuery.SetField("urn")
	exactUrnQuery.SetBoost(100.0)

	// BLV-5: match against the non-ngram name_exact keyword field. Targeting "name"
	// (edge-ngrammed) never produced a single matchable term, so this high-boost leg
	// was dead.
	exactNameQuery := bleve.NewTermQuery(strings.ToLower(queryStr))
	exactNameQuery.SetField("name_exact")
	exactNameQuery.SetBoost(50.0)

	exactQualifiedQuery := bleve.NewTermQuery(strings.ToLower(queryStr))
	exactQualifiedQuery.SetField("qualified_name")
	exactQualifiedQuery.SetBoost(75.0)

	matchQuery := buildFuzzyTokenMatchQuery(queryStr, tokens)

	descMatchQuery := bleve.NewMatchQuery(queryStr)
	descMatchQuery.SetField("description")
	descMatchQuery.SetBoost(1.5)

	liteSummaryMatchQuery := bleve.NewMatchQuery(queryStr)
	liteSummaryMatchQuery.SetField("lite_summary")
	liteSummaryMatchQuery.SetBoost(2.0)

	intentQuery := bleve.NewMatchQuery(queryStr)
	intentQuery.SetField("intent")
	intentQuery.SetBoost(3.0)

	nameMatchQuery := bleve.NewMatchQuery(queryStr)
	nameMatchQuery.SetField("name")
	nameMatchQuery.SetFuzziness(1)
	nameMatchQuery.SetBoost(4.0)

	var prefixQuery query.Query
	if len(parts) > 0 {
		pr := bleve.NewPrefixQuery(strings.ToLower(parts[0]))
		pr.SetField("name")
		pr.SetBoost(2.0)
		prefixQuery = pr
	} else {
		prefixQuery = bleve.NewMatchNoneQuery()
	}

	// BLV-5: wildcard against name_exact (a single stored term) so multi-word
	// patterns like "search*tools*" can match "search_tools"; against the ngram
	// "name" field they matched nothing.
	wildtext := strings.ToLower(strings.Join(parts, "*")) + "*"
	wildcardQuery := bleve.NewWildcardQuery(wildtext)
	wildcardQuery.SetField("name_exact")
	wildcardQuery.SetBoost(2.0)

	synthIntentQuery := bleve.NewMatchQuery(queryStr)
	synthIntentQuery.SetField("synthetic_intents")
	synthIntentQuery.SetBoost(3.0)

	lexicalQuery := buildTokenizedKeywordQuery(tokens, "lexical_tokens", 3.0)

	paramQuery := bleve.NewMatchQuery(queryStr)
	paramQuery.SetField("parameter_names")
	paramQuery.SetBoost(2.5)

	enumQuery := bleve.NewMatchQuery(queryStr)
	enumQuery.SetField("enum_values")
	enumQuery.SetBoost(2.0)

	disjunctParts := []query.Query{
		exactUrnQuery, exactQualifiedQuery, exactNameQuery, matchQuery,
		descMatchQuery, liteSummaryMatchQuery, intentQuery, nameMatchQuery,
		prefixQuery, wildcardQuery, synthIntentQuery, lexicalQuery, paramQuery, enumQuery,
	}
	disjunctParts = append(disjunctParts, buildPhraseQueries(queryStr, parts)...)
	disjunctParts = append(disjunctParts, buildDAGDiscoveryQueries(queryStr)...)

	core := bleve.NewDisjunctionQuery(disjunctParts...)

	wrapped := bleve.NewBooleanQuery()
	wrapped.AddMust(core)
	excluded := false
	for _, token := range tokens {
		if len(token) < 2 {
			continue
		}
		nq := bleve.NewTermQuery(strings.ToLower(token))
		nq.SetField("negative_triggers")
		wrapped.AddMustNot(nq)
		excluded = true
	}
	if excluded {
		return wrapped
	}
	return core
}
