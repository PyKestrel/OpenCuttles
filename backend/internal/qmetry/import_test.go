package qmetry

import "testing"

// A QMetry-style export: case-level columns on the first row of each case,
// blank on subsequent step rows; mixed-case + aliased headers; a blank row.
const fixtureCSV = `Test Case Key,Summary,Precondition,Priority,Labels,Folder,Step Summary,Test Data,Expected Result
TC-1,Login succeeds,App is installed,High,"smoke, auth",Auth/Login,Open the app,,Login screen is shown
,,,,,,Enter valid credentials,user@x.com / pw,Home screen is shown
,,,,,,Tap the profile icon,,Profile page opens

TC-2,Logout succeeds,User is logged in,Medium,auth,Auth/Login,Open the menu,,Menu is visible
,,,,,,Tap Log out,,Login screen is shown
`

func TestParseQMetryCSV(t *testing.T) {
	res, err := ParseFile("export.csv", []byte(fixtureCSV))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.CasesParsed != 2 {
		t.Fatalf("cases = %d, want 2 (warnings: %v)", res.CasesParsed, res.Warnings)
	}
	if res.StepsParsed != 5 {
		t.Fatalf("steps = %d, want 5", res.StepsParsed)
	}

	c1 := res.Cases[0]
	if c1.Summary != "Login succeeds" || c1.Precondition != "App is installed" || c1.Priority != "High" {
		t.Fatalf("case1 fields wrong: %+v", c1)
	}
	if c1.FolderPath != "Auth/Login" {
		t.Fatalf("folder = %q", c1.FolderPath)
	}
	if len(c1.Labels) != 2 || c1.Labels[0] != "smoke" || c1.Labels[1] != "auth" {
		t.Fatalf("labels = %v", c1.Labels)
	}
	if c1.ExternalKey != "TC-1" {
		t.Fatalf("externalKey = %q", c1.ExternalKey)
	}
	if len(c1.Steps) != 3 {
		t.Fatalf("case1 steps = %d, want 3", len(c1.Steps))
	}
	if c1.Steps[0].Action != "Open the app" || c1.Steps[0].Expected != "Login screen is shown" || c1.Steps[0].Index != 0 {
		t.Fatalf("step0 wrong: %+v", c1.Steps[0])
	}
	if c1.Steps[1].TestData != "user@x.com / pw" || c1.Steps[1].Index != 1 {
		t.Fatalf("step1 wrong: %+v", c1.Steps[1])
	}

	c2 := res.Cases[1]
	if c2.Summary != "Logout succeeds" || len(c2.Steps) != 2 {
		t.Fatalf("case2 wrong: %+v", c2)
	}
}

func TestParseQMetryHeaderAliases(t *testing.T) {
	// "Title"/"Action"/"Expected" aliases; tab-free, different casing/spacing.
	csv := "Title,Action,Expected\n" +
		"Case A,do something,it works\n" +
		",do another,still works\n"
	res, err := ParseFile("x.csv", []byte(csv))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if res.CasesParsed != 1 || res.StepsParsed != 2 {
		t.Fatalf("cases=%d steps=%d warnings=%v", res.CasesParsed, res.StepsParsed, res.Warnings)
	}
	if res.Cases[0].Steps[1].Action != "do another" {
		t.Fatalf("alias step wrong: %+v", res.Cases[0].Steps)
	}
}

func TestParseNoSummaryColumn(t *testing.T) {
	res, _ := ParseFile("x.csv", []byte("Foo,Bar\n1,2\n"))
	if res.CasesParsed != 0 || len(res.Warnings) == 0 {
		t.Fatalf("expected no cases + a warning, got %+v", res)
	}
}
