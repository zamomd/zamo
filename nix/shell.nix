{
  lib,
  stdenv,
  mkShell,
  writeShellScriptBin,
  go_1_27,
  golangci-lint,
  just,
  bun,
  git-cliff,
  pkg-config,
  gtk4,
  webkitgtk_6_0,
}:

let
  wails3 = writeShellScriptBin "wails3" ''
    exec go run "github.com/wailsapp/wails/v3/cmd/wails3@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)" "$@"
  '';
in
mkShell {
  packages = [
    go_1_27
    golangci-lint
    just
    bun
    git-cliff
    wails3
  ]
  ++ lib.optionals stdenv.hostPlatform.isLinux [
    pkg-config
    gtk4
    webkitgtk_6_0
  ];
}
