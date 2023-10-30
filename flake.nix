{
  description = "Woodpecker CI external configuration service";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

    # <https://github.com/nix-systems/nix-systems>
    systems.url = "github:nix-systems/default-linux";
  };

  outputs = inputs @ {
    self,
    nixpkgs,
    systems,
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

     # Nixos module
    nixosModules.woodpecker-config-service = { pkgs, lib, config, ... }:
      with lib;
      let cfg = config.services.woodpecker-config-service;
      in {
        meta.maintainers = with lib.maintainers; [ strumtrar ];
        options.services.woodpecker-config-service = {
          package = lib.mkPackageOptionMD pkgs "woodpecker-config-service" { };
          enable = lib.mkEnableOption (lib.mdDoc description);
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

            path = with pkgs; [
              bash
              coreutils
              git
              git-lfs
              gnutar
              gzip
              nix
            ];

            description = "Woodpecker Configuration Service";
            wantedBy = [ "multi-user.target" ];
            after = [ "network-online.target" ];
            wants = [ "network-online.target" ];

            serviceConfig = {

              RuntimeDirectory = "woodpecker-config-service";
              User = "woodpecker-config-service";
              DynamicUser = true;
              SupplementaryGroups = cfg.extraGroups;
              ExecStart = lib.getExe cfg.package;
              Restart = "on-failure";
              RestartSec = 15;
              CapabilityBoundingSet = "";
              NoNewPrivileges = true;
              ProtectSystem = "strict";
              PrivateTmp = true;
              PrivateDevices = true;
              PrivateUsers = true;
              ProtectHostname = true;
              ProtectClock = true;
              ProtectKernelTunables = true;
              ProtectKernelModules = true;
              ProtectKernelLogs = true;
              ProtectControlGroups = true;
              RestrictAddressFamilies = [ "AF_UNIX AF_INET AF_INET6" ];
              LockPersonality = true;
              MemoryDenyWriteExecute = true;
              RestrictRealtime = true;
              RestrictSUIDSGID = true;
              PrivateMounts = true;
              SystemCallArchitectures = "native";
              SystemCallFilter = "~@clock @privileged @cpu-emulation @debug @keyring @module @mount @obsolete @raw-io @reboot @swap";
              BindPaths = [
                "/nix/var/nix/daemon-socket/socket"
                "/run/nscd/socket"
              ];
              BindReadOnlyPaths = [
                "${config.environment.etc."ssh/ssh_known_hosts".source}:/etc/ssh/ssh_known_hosts"
                "-/etc/hosts"
                "-/etc/localtime"
                "-/etc/nsswitch.conf"
                "-/etc/resolv.conf"
                "-/etc/ssl/certs"
                "-/etc/static/ssl/certs"
                "/etc/group:/etc/group"
                "/etc/machine-id"
                "/etc/nix:/etc/nix"
                "/etc/passwd:/etc/passwd"
                # channels are dynamic paths in the nix store, therefore we need to bind mount the whole thing
                "/nix/"
              ];
            };
          };
        };
      };

    formatter = eachSystem (system: nixpkgs.legacyPackages.${system}.alejandra);

  };
}
