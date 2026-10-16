#!/bin/sh
# build, install, or run the jira CLI.
# install copies the binary to /usr/local/bin/jira and creates
# ~/.go-jira/go-jirarc when that file is absent.
set -eu
cd "$(dirname "$0")"

bin="./jira"
dest="/usr/local/bin/jira"
cfg_dir="${HOME:?HOME is not set}/.go-jira"
cfg="$cfg_dir/go-jirarc"

usage() {
	echo "usage: ./build.sh build|install|run [args...]" >&2
	exit 2
}

build() {
	go build -o "$bin" ./cmd/jira
}

install_config() {
	mkdir -p "$cfg_dir"
	chmod 700 "$cfg_dir"
	if [ -e "$cfg" ]; then
		return 0
	fi
	umask 077
	cat > "$cfg" << 'EOF'
base_url = ""
email = ""
output = "json"
api_token = ""
EOF
	chmod 600 "$cfg"
}

case "${1:-}" in
build)
	build
	;;
install)
	build
	if [ -w /usr/local/bin ]; then
		install -m 755 "$bin" "$dest"
	else
		sudo install -m 755 "$bin" "$dest"
	fi
	install_config
	printf 'installed %s\nconfig %s\n' "$dest" "$cfg"
	;;
run)
	build
	shift
	exec "$bin" "$@"
	;;
*)
	usage
	;;
esac
