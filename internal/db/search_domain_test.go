package db

import "testing"

func TestRecordAllowedInSearch(t *testing.T) {
	t.Parallel()

	recallTool := &ToolRecord{URN: "recall:search", Server: "recall", Category: "memory"}
	brainstormTool := &ToolRecord{URN: "brainstorm:think", Server: "brainstorm", Category: "pipeline"}

	if !recordAllowedInSearch(recallTool, "", "", "", DomainUserLand) {
		t.Fatal("recall should be visible in user land")
	}
	if recordAllowedInSearch(brainstormTool, "", "", "", DomainUserLand) {
		t.Fatal("brainstorm should be masked in user land")
	}
	if !recordAllowedInSearch(brainstormTool, "brainstorm", "", "", DomainUserLand) {
		t.Fatal("brainstorm should be visible when query targets it")
	}
	if !recordAllowedInSearch(brainstormTool, "", "", "", DomainPipelineOrchestration) {
		t.Fatal("brainstorm should be visible in pipeline domain")
	}
	if recordAllowedInSearch(recallTool, "", "memory", "other", DomainUserLand) {
		t.Fatal("server constraint should filter")
	}
}

func TestToBleveDocQualifiedName(t *testing.T) {
	t.Parallel()
	doc := ToBleveDoc(&ToolRecord{URN: "ddg-search:search_web", Server: "ddg-search", Name: "search_web"})
	if doc.QualifiedName != "ddg-search:search_web" {
		t.Fatalf("qualified_name = %q", doc.QualifiedName)
	}
}
