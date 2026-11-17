package cli

import (
	"bytes"

	"os"

	"path/filepath"

	"testing"
)

func TestServeSkillPrivateDeploymentAndVerification(t *testing.T) {
	project := cliProject(t)
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "native-fixture")
	if err := os.WriteFile(source, []byte("current platform fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	plan, err := planServeSkill(project, "", source)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.managed {
		t.Fatal("expected private managed Skill")
	}
	if err := plan.deploy(project); err != nil {
		t.Fatal(err)
	}
	if err := plan.deploy(project); err != nil {
		t.Fatal("cache not reusable:", err)
	}
	data, err := os.ReadFile(filepath.Join(plan.directory, "bin", nativeCLIName()))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "current platform fixture" {
		t.Fatal("wrong executable copy")
	}
	if err := outsideWorkspace(project, plan.directory); err != nil {
		t.Fatal(err)
	}
	rp, err := os.ReadFile(filepath.Join(project, "rp.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rp, []byte("skills")) {
		t.Fatal("runtime Skill leaked into RP config")
	}
	skillFile := filepath.Join(plan.directory, "SKILL.md")
	original, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillFile, append(original, []byte("\nmodified")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := plan.deploy(project); err == nil {
		t.Fatal("accepted modified generated instructions")
	}
	os.WriteFile(skillFile, original, 0600)
	os.WriteFile(filepath.Join(plan.directory, "bin", nativeCLIName()), []byte("changed"), 0700)
	if err := plan.deploy(project); err == nil {
		t.Fatal("accepted modified generated executable")
	}
	os.WriteFile(source, []byte("new executable version"), 0700)
	next, err := planServeSkill(project, "", source)
	if err != nil {
		t.Fatal(err)
	}
	if next.directory == plan.directory {
		t.Fatal("different executable reused same cache")
	}
	if err := next.deploy(project); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.directory); err != nil {
		t.Fatal("new version removed active old version")
	}
}
func TestServeSkillExplicitDirectoryAndPathValidation(t *testing.T) {
	project := cliProject(t)
	t.Setenv("RPS_RUNTIME_DIR", t.TempDir())
	source := filepath.Join(t.TempDir(), "native-fixture")
	os.WriteFile(source, []byte("fixture"), 0700)
	installed, err := installSkill(t.TempDir(), source, false)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planServeSkill(project, installed, source)
	if err != nil {
		t.Fatal(err)
	}
	if plan.managed || plan.directory != installed {
		t.Fatal(plan)
	}
	if _, err := planServeSkill(project, project, source); err == nil {
		t.Fatal("trusted Skill accepted inside RP data")
	}
	os.WriteFile(filepath.Join(installed, "SKILL.md"), []byte("---\nname: other\ndescription: no\n---\ntext"), 0600)
	if _, err := planServeSkill(project, installed, source); err == nil {
		t.Fatal("accepted different Skill identity")
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(installed, link); err == nil {
		if _, err := planServeSkill(project, link, source); err == nil {
			t.Fatal("accepted linked Skill root")
		}
	}
}

func TestManagedSkillRejectsWorkspaceTempWithoutWriting(t *testing.T) {
	project := cliProject(t)
	source := filepath.Join(t.TempDir(), "native-fixture")
	if err := os.WriteFile(source, []byte("fixture"), 0700); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(project)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", project)
	t.Setenv("TMP", project)
	t.Setenv("TEMP", project)
	plan, err := planServeSkill(project, "", source)
	if err == nil {
		os.RemoveAll(filepath.Dir(plan.directory))
		t.Fatal("managed Skill accepted workspace temporary directory")
	}
	after, err := os.ReadDir(project)
	if err != nil || len(after) != len(before) {
		t.Fatal("failed planning changed workspace", after, err)
	}
}
