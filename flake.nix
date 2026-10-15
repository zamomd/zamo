{
  description = "Zamo — living knowledge base";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    bun2nix = {
      url = "github:nix-community/bun2nix/2.1.2";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      bun2nix,
    }:
    let
      inherit (nixpkgs) lib;
      systems = [
        "aarch64-darwin"
        "x86_64-linux"
        "aarch64-linux"
      ];
      linuxSystems = lib.intersectLists lib.platforms.linux systems;
      forSystems = systems: f: lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});

      version = "git-${self.shortRev or self.dirtyShortRev or "unknown"}";
    in
    {
      packages = forSystems linuxSystems (pkgs: rec {
        zamo = pkgs.callPackage ./nix/package.nix {
          inherit version;
          bun2nix = bun2nix.packages.${pkgs.stdenv.hostPlatform.system}.default;
        };
        default = zamo;
      });

      devShells = forSystems systems (pkgs: {
        default = pkgs.callPackage ./nix/shell.nix { };
      });
    };
}
