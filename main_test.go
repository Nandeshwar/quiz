package main

import "testing"

func TestLoadDatasets(t *testing.T) {
	v1, err := loadDataset("data/v1.json")
	if err != nil {
		t.Fatalf("load v1: %v", err)
	}
	v2, err := loadDataset("data/v2.json")
	if err != nil {
		t.Fatalf("load v2: %v", err)
	}

	if got := len(v1.Questions); got != 128 {
		t.Fatalf("v1 questions = %d, want 128", got)
	}
	if got := len(v2.Questions); got != 100 {
		t.Fatalf("v2 questions = %d, want 100", got)
	}
}

func TestMatchesAnswerSingleAndMulti(t *testing.T) {
	single := question{
		ID:              61,
		Question:        "Who is the governor of your state now?",
		Answers:         []string{"Jared Polis", "Governor Jared Polis"},
		RequiredAnswers: 1,
	}
	if !matchesAnswer(single, "Jared Polis") {
		t.Fatal("expected single-answer match")
	}

	multi := question{
		ID:              67,
		Question:        "Name two promises that new citizens make in the Oath of Allegiance.",
		Answers:         []string{"Give up loyalty to other countries", "Defend the Constitution", "Obey the laws of the United States"},
		RequiredAnswers: 2,
	}
	if !matchesAnswer(multi, "Give up loyalty to other countries, obey the laws of the United States") {
		t.Fatal("expected multi-answer match")
	}
	if matchesAnswer(multi, "Defend the Constitution") {
		t.Fatal("expected insufficient answers to fail")
	}
}

func TestEquivalentAllowsSeventyFivePercentWordMatchIgnoringCase(t *testing.T) {
	if !equivalent("father of constitution", `"Father of the Constitution"`) {
		t.Fatal("expected 75 percent word match to pass")
	}
	if equivalent("father liberty", `"Father of the Constitution"`) {
		t.Fatal("expected less than 75 percent word match to fail")
	}
	if !equivalent("JARED POLIS", "Jared Polis") {
		t.Fatal("expected case-insensitive match to pass")
	}
	if equivalent("citizens in their states", "Citizens in their district") {
		t.Fatal("expected different meaningful word to fail")
	}
}

func TestMatchesAnswerDoesNotAcceptBroaderPhraseForEmbeddedAnswer(t *testing.T) {
	q := question{
		ID:              66,
		Question:        "What do we show loyalty to when we say the Pledge of Allegiance?",
		Answers:         []string{"The United States", "The flag"},
		RequiredAnswers: 1,
	}

	if matchesAnswer(q, "The president of the United States") {
		t.Fatal("expected broader phrase containing accepted answer text to fail")
	}
	if !matchesAnswer(q, "The United States") {
		t.Fatal("expected exact accepted answer to pass")
	}
}

func TestMatchesAnswerAllowsCommonTitleForPersonName(t *testing.T) {
	q := question{
		ID:              61,
		Question:        "Who is one of your state's U.S. Senators now?",
		Answers:         []string{"Michael Bennet"},
		RequiredAnswers: 1,
	}

	if !matchesAnswer(q, "Senator Michael Bennet") {
		t.Fatal("expected title-prefixed person name to pass")
	}
}
