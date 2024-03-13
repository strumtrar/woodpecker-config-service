{
  description = "Woodpecker CI external configuration service";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    utils.url = "github:numtide/flake-utils";

    # <https://github.com/nix-systems/nix-systems>
    systems.url = "github:nix-systems/default-linux";
  };

  outputs = inputs @ {
    self,
    nixpkgs,
    systems,
    utils,
    ...
  }: let
    inherit (nixpkgs) lib;
    eachSystem = lib.genAttrs (import systems);
    pkgsFor = eachSystem (system:
      import nixpkgs {
        localSystem = system;
        overlays = with self.overlays; [
	  default
        ];
      });
  in {
    overlays.default = final: prev: {
      woodpecker-config-service = with final;
        buildGoModule {

          pname = "woodpecker-config-service";
          version = "v1.0.0";
          src = fetchFromGitHub {
	    owner = "woodpecker-ci";
	    repo = "example-config-service";
	    rev = "master";
	    sha256 = "sha256-DTjotK0yrho6zBTfpRHxeUoIWlHUxtKHOa9k1/pAxoQ=";
	  };
	  vendorSha256 = "sha256-eHqNjjs5Pa+WTDvdR8kmoBELwciJEyNUuG9kROk91Ig=";

	  CGO_ENABLED = 1;

          meta = with lib; {
  	    description = "Woodpecker CI external configuration service";
            homepage = "https://github.com/woodpecker-ci/example-config-service";
            license = licenses.gpl3;
            mainProgram = "example-config-service";
            maintainers = with maintainers; [ strumtrar ];
          };
        };
    };

    packages = eachSystem (system: {
      default = self.packages.${system}.woodpecker-config-service;
      inherit
        (pkgsFor.${system})
        woodpecker-config-service;
    });

    defaultApp = utils.lib.mkApp { drv = self.packages.x86_64-linux.default; };

     # Nixos module
    nixosModules.woodpecker-config-service = { pkgs, lib, config, ... }:
      with lib;
      let cfg = config.services.woodpecker-config-service;
      in {
        meta.maintainers = with lib.maintainers; [ strumtrar ];
        options.services.woodpecker-config-service = {
          enable = lib.mkEnableOption (lib.mdDoc description);
          package = lib.mkPackageOptionMD pkgs "woodpecker-config-service" { };
          extraGroups = lib.mkOption {
            type = lib.types.listOf lib.types.str;
            default = [ ];
            example = [ "podman" ];
            description = lib.mdDoc ''
              Additional groups for the systemd service.
            '';
          };

          environment = lib.mkOption {
            default = { };
            type = lib.types.attrsOf lib.types.str;
            example = lib.literalExpression
              ''
                {
                  CONFIG_SERVICE_PUBLIC_KEY_FILE = "/path/to/key.txt";
                  CONFIG_SERVICE_HOST = "localhost:8000";
                  CONFIG_SERVICE_OVERRIDE_FILTER = "test-*"
                }
              '';
          };
        };

        config = lib.mkIf cfg.enable {
          nixpkgs.overlays = [ self.overlays.default ];

          # Allow user to run nix
          nix.settings.allowed-users = [ "woodpecker-config-service" ];

          # Service
          systemd.services.woodpecker-config-service = {
            environment = cfg.environment // {
              HOME = "/run/woodpecker-config-service";
            };
            description = "Woodpecker Configuration Service";
            wantedBy = [ "multi-user.target" ];
            after = [ "network-online.target" ];
            wants = [ "network-online.target" ];

            serviceConfig = {
              User = "woodpecker-config-service";
              RuntimeDirectory = "woodpecker-config-service";
              SupplementaryGroups = cfg.extraGroups;
              ExecStart = lib.getExe cfg.package;
              Restart = "on-failure";
              RestartSec = 15;
            };
          };
        };
      };

    formatter = eachSystem (system: nixpkgs.legacyPackages.${system}.alejandra);

  };
}
