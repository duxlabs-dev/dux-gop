# Spec for dgop - uses rpkg macros for both stable and git builds

%global debug_package %{nil}
%global version {{{ git_dir_version }}}
%global pkg_summary System monitoring CLI and REST API

Name:           dgop
Version:        %{version}
Release:        1%{?dist}
Summary:        %{pkg_summary}

License:        MIT
URL:            https://github.com/duxlabs-dev/dux-gop
VCS:            {{{ git_dir_vcs }}}
Source0:        {{{ git_dir_pack }}}

BuildRequires:  git-core
BuildRequires:  golang >= 1.21
BuildRequires:  rpkg

Requires:       glibc

%description
dgop is a go-based stateless system monitoring tool that provides both a CLI interface
and REST API for retrieving system metrics including CPU, memory, disk, network,
processes, and GPU information.

Features:
- Interactive TUI with real-time system monitoring
- REST API server with OpenAPI specification
- JSON output for all metrics
- GPU temperature monitoring (NVIDIA)
- Lightweight single-binary deployment

%prep
{{{ git_dir_setup_macro }}}

%build
export CGO_CPPFLAGS="${CPPFLAGS}"
export CGO_CFLAGS="${CFLAGS}"
export CGO_CXXFLAGS="${CXXFLAGS}"
export CGO_LDFLAGS="${LDFLAGS}"
export GOFLAGS="-buildmode=pie -trimpath -mod=readonly -modcacherw"

# Build the binary
go build -v -o dgop ./cmd/dgop

%install
install -Dm755 dgop %{buildroot}%{_bindir}/dgop

%files
%license LICENSE
%doc README.md
%{_bindir}/dgop

%changelog
{{{ git_dir_changelog }}}