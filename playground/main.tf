provider "remote" {
  max_sessions = 2

  conn {
    host     = "localhost"
    port     = 8022
    user     = "root"
    password = "password"
    sudo     = false
  }
}

provider "remote" {
  alias = "proxied"

  conn {
    host     = "remotehost2"
    port     = 22
    user     = "root"
    password = "password"
    sudo     = false
  }

  proxy_conn {
    host     = "localhost"
    port     = 8022
    user     = "root"
    password = "password"
    sudo     = false
  }
}

resource "remote_file" "foo" {
  path        = "/tmp/.bashrc"
  content     = "alias ll='ls -alF'"
  permissions = "0644"
  owner       = "0"
  group       = "0"
}

resource "remote_file" "bar" {
  provider = remote.proxied

  path    = "/tmp/hello.txt"
  content = "Hello, World!"
}
