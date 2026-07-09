{
  description = "Comind — living knowledge base";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { nixpkgs, ... }:
    let
      systems = [
        "aarch64-darwin"
        "x86_64-darwin"
        "x86_64-linux"
        "aarch64-linux"
      ];
      eachSystem = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
    in
    {
      devShells = eachSystem (pkgs: {
        default = pkgs.mkShell {
          packages =
            with pkgs;
            [
              go
              golangci-lint
              just
              bun
              git-cliff
            ]
            ++ pkgs.lib.optionals pkgs.stdenv.isLinux [
              pkg-config
              gtk4
              webkitgtk_6_0
            ];

          # The wails3 CLI is not in nixpkgs (v3 is alpha).
          # go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha2.114
        };
      });
    };
}
