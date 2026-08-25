package transfer

import (
	"strings"
	"testing"
)

func TestSSHFSInputPathMapsSeedboxPathBelowReadOnlyMount(t *testing.T) {
	client, err := New(Config{
		Type: "sshfs", Host: "seedbox.example", User: "worker", Port: 22,
		SourceRoot: "/downloads", MountPath: "/mnt/gonzb-seedbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	remote, name, err := client.ResolveSource("/downloads/tv/Release.Name", "", "ignored")
	if err != nil {
		t.Fatal(err)
	}
	input, err := client.InputPath("/var/lib/gonzb-worker/jobs/id", remote, name)
	if err != nil {
		t.Fatal(err)
	}
	if input != "/mnt/gonzb-seedbox/tv/Release.Name" {
		t.Fatalf("input path=%q", input)
	}
}

func TestParseAndValidateReadOnlySSHFSFromLinuxMountInfo(t *testing.T) {
	line := `42 31 0:58 / /mnt/gonzb\040seedbox ro,nosuid,nodev,relatime - fuse.sshfs worker@seedbox:/downloads rw,user_id=1000,group_id=1000`
	info, ok := parseMountInfoLine(line)
	if !ok {
		t.Fatal("mountinfo line was not parsed")
	}
	if info.MountPath != "/mnt/gonzb seedbox" || info.FSType != "fuse.sshfs" || info.Source != "worker@seedbox:/downloads" || !info.Options["ro"] {
		t.Fatalf("unexpected mount info: %+v", info)
	}
	if err := validateMountedSSHFS(info); err != nil {
		t.Fatal(err)
	}
	info.Options = map[string]bool{"rw": true}
	if err := validateMountedSSHFS(info); err == nil {
		t.Fatal("expected writable SSHFS mount to be rejected")
	}
}

func TestSSHFSRejectsOptionsThatCouldExposeOrModifySeedboxFiles(t *testing.T) {
	for _, option := range []string{"rw", "allow_other", "allow_root", "password_stdin", "ssh_command=/tmp/wrapper", "reconnect,allow_other"} {
		_, err := New(Config{
			Type: "sshfs", Host: "seedbox.example", User: "worker", Port: 22,
			SourceRoot: "/downloads", MountPath: "/mnt/gonzb-seedbox", SSHFSOptions: []string{option},
		})
		if err == nil {
			t.Fatalf("expected SSHFS option %q to be rejected", option)
		}
	}
}

func TestSSHFSArgumentsForceReadOnlyReconnectAndKeyAuthentication(t *testing.T) {
	client, err := New(Config{
		Type: "sshfs", Host: "seedbox.example", User: "worker", Port: 2222,
		SourceRoot: "/downloads", MountPath: "/mnt/gonzb-seedbox", KeyPath: "/keys/worker",
		SSHFSOptions: []string{"cache=yes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	args, err := client.sshfsArguments()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"worker@seedbox.example:/downloads", "/mnt/gonzb-seedbox", "-p 2222",
		"-o ro", "-o BatchMode=yes", "-o reconnect", "-o IdentityFile=/keys/worker", "-o cache=yes",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("SSHFS arguments missing %q: %s", expected, joined)
		}
	}
}
