package provider

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/bramvdbogaerde/go-scp"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Error struct {
	cmd    string
	err    error
	stderr []byte
}

func (e Error) Error() string {
	stderr := strings.TrimRight(string(e.stderr), "\n")
	return fmt.Sprintf("`%s`\n  %s\n  %s", e.cmd, e.err, stderr)
}

func run(s *ssh.Session, cmd string) error {
	var b bytes.Buffer
	s.Stderr = &b
	err := s.Run(cmd)

	if err != nil {
		return Error{
			cmd:    cmd,
			err:    err,
			stderr: b.Bytes(),
		}
	}
	return nil
}

type RemoteClient struct {
	sshClient *ssh.Client
}

func (c *RemoteClient) WriteFile(content string, path string, permissions string, sudo bool) error {
	if sudo {
		return c.WriteFileShell(content, path)
	}
	return c.WriteFileSCP(content, path, permissions)
}

func (c *RemoteClient) WriteFileSCP(content string, path string, permissions string) error {
	scpClient, err := c.GetSCPClient()
	if err != nil {
		return err
	}
	defer scpClient.Close()

	return scpClient.CopyFile(strings.NewReader(content), path, permissions)
}

func (c *RemoteClient) WriteFileShell(content string, path string) error {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}

	go func() {
		stdin.Write([]byte(content))
		stdin.Close()
	}()

	cmd := fmt.Sprintf("cat /dev/stdin | sudo tee %s", path)
	return run(session, cmd)
}

func (c *RemoteClient) ChmodFile(path string, permissions string, sudo bool) error {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	cmd := fmt.Sprintf("chmod %s %s", permissions, path)
	if sudo {
		cmd = fmt.Sprintf("sudo %s", cmd)
	}
	return run(session, cmd)
}

func (c *RemoteClient) ChgrpFile(path string, group string, sudo bool) error {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	cmd := fmt.Sprintf("chgrp %s %s", group, path)
	if sudo {
		cmd = fmt.Sprintf("sudo %s", cmd)
	}

	return run(session, cmd)
}

func (c *RemoteClient) ChownFile(path string, owner string, sudo bool) error {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	cmd := fmt.Sprintf("chown %s %s", owner, path)
	if sudo {
		cmd = fmt.Sprintf("sudo %s", cmd)
	}
	return run(session, cmd)
}

func (c *RemoteClient) FileExists(path string, sudo bool) (bool, error) {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return false, err
	}
	defer session.Close()

	cmd := fmt.Sprintf("test -f %s", path)
	if sudo {
		cmd = fmt.Sprintf("sudo %s", cmd)
	}
	err = run(session, cmd)

	if err != nil {
		session2, err := sshClient.NewSession()
		if err != nil {
			return false, err
		}
		defer session2.Close()

		cmd := fmt.Sprintf("test ! -f %s", path)
		if sudo {
			cmd = fmt.Sprintf("sudo %s", cmd)
		}
		return false, session2.Run(cmd)
	}

	return true, nil
}

func (c *RemoteClient) ReadFile(path string, sudo bool) (string, error) {
	if sudo {
		return c.ReadFileShell(path)
	}
	return c.ReadFileSFTP(path)
}

func (c *RemoteClient) ReadFileSFTP(path string) (string, error) {
	sftpClient, err := c.GetSFTPClient()
	if err != nil {
		return "", err
	}
	defer sftpClient.Close()

	file, err := sftpClient.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	content := bytes.Buffer{}
	_, err = file.WriteTo(&content)
	if err != nil {
		return "", err
	}

	return content.String(), nil
}

func (c *RemoteClient) ReadFileShell(path string) (string, error) {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	cmd := fmt.Sprintf("sudo cat %s", path)
	content, err := session.Output(cmd)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func (c *RemoteClient) ReadFilePermissions(path string, sudo bool) (string, error) {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	cmd := fmt.Sprintf("stat -c %%a %s", path)
	if sudo {
		cmd = fmt.Sprintf("sudo %s", cmd)
	}
	output, err := session.Output(cmd)
	if err != nil {
		return "", err
	}

	permissions := strings.ReplaceAll(string(output), "\n", "")
	if len(permissions) > 0 && len(permissions) < 4 {
		permissions = fmt.Sprintf("0%s", permissions)
	}
	return permissions, nil
}

func (c *RemoteClient) ReadFileOwner(path string, sudo bool) (string, error) {
	return c.StatFile(path, "u", sudo)
}

func (c *RemoteClient) ReadFileGroup(path string, sudo bool) (string, error) {
	return c.StatFile(path, "g", sudo)
}

func (c *RemoteClient) ReadFileOwnerName(path string, sudo bool) (string, error) {
	return c.StatFile(path, "U", sudo)
}

func (c *RemoteClient) ReadFileGroupName(path string, sudo bool) (string, error) {
	return c.StatFile(path, "G", sudo)
}

func (c *RemoteClient) StatFile(path string, char string, sudo bool) (string, error) {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	cmd := fmt.Sprintf("stat -c %%%s %s", char, path)
	if sudo {
		cmd = fmt.Sprintf("sudo %s", cmd)
	}
	output, err := session.Output(cmd)
	if err != nil {
		return "", err
	}

	group := strings.ReplaceAll(string(output), "\n", "")
	return group, nil
}

func (c *RemoteClient) DeleteFile(path string, sudo bool) error {
	if sudo {
		return c.DeleteFileShell(path)
	}
	return c.DeleteFileSFTP(path)
}

func (c *RemoteClient) DeleteFileSFTP(path string) error {
	sftpClient, err := c.GetSFTPClient()
	if err != nil {
		return err
	}
	defer sftpClient.Close()

	return sftpClient.Remove(path)
}

func (c *RemoteClient) DeleteFileShell(path string) error {
	sshClient := c.GetSSHClient()

	session, err := sshClient.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	cmd := fmt.Sprintf("sudo rm %s", path)
	return run(session, cmd)
}

func NewRemoteClient(host string, clientConfig *ssh.ClientConfig) (*RemoteClient, error) {
	client, err := ssh.Dial("tcp", host, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("couldn't establish a connection to the remote server: %s", err.Error())
	}

	return &RemoteClient{
		sshClient: client,
	}, nil
}

func NewRemoteProxyClient(host string, clientConfig *ssh.ClientConfig, proxyHost string, proxyClientConfig *ssh.ClientConfig) (*RemoteClient, error) {
	proxyClient, err := ssh.Dial("tcp", proxyHost, proxyClientConfig)
	if err != nil {
		return nil, fmt.Errorf("couldn't establish a connection to the remote server: %s", err.Error())
	}

	conn, err := proxyClient.Dial("tcp", host)
	if err != nil {
		return nil, fmt.Errorf("couldn't establish a connection to the remote server: %s", err.Error())
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, host, clientConfig)
	if err != nil {
		return nil, err
	}
	client := ssh.NewClient(ncc, chans, reqs)

	return &RemoteClient{
		sshClient: client,
	}, nil
}

func (c *RemoteClient) Close() error {
	return c.sshClient.Close()
}

func (c *RemoteClient) GetSSHClient() *ssh.Client {
	return c.sshClient
}

func (c *RemoteClient) GetSCPClient() (scp.Client, error) {
	return scp.NewClientBySSH(c.sshClient)
}

func (c *RemoteClient) GetSFTPClient() (*sftp.Client, error) {
	return sftp.NewClient(c.sshClient)
}
