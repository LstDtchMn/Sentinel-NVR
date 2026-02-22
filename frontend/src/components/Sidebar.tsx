/**
 * Sidebar — persistent navigation sidebar (CG11).
 * Shows app logo, nav links, and version fetched from the API.
 */
import { useEffect, useState } from "react";
import { NavLink } from "react-router-dom";
import { LayoutDashboard, Camera, Settings, Shield } from "lucide-react";
import { api } from "../api/client";

const navItems = [
  { to: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { to: "/cameras", label: "Cameras", icon: Camera },
  { to: "/settings", label: "Settings", icon: Settings },
];

export default function Sidebar() {
  const [version, setVersion] = useState<string>("");

  useEffect(() => {
    const controller = new AbortController();
    api
      .getHealth(controller.signal)
      .then((h) => setVersion(h.version))
      .catch(() => {});
    return () => controller.abort();
  }, []);

  return (
    <aside className="w-64 bg-surface-raised border-r border-border flex flex-col">
      {/* Logo */}
      <div className="h-16 flex items-center px-6 border-b border-border">
        <Shield className="w-6 h-6 text-sentinel-500 mr-3" />
        <span className="text-lg font-semibold tracking-tight">Sentinel NVR</span>
      </div>

      {/* Navigation */}
      <nav className="flex-1 py-4 px-3 space-y-1">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            className={({ isActive }) =>
              `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors ${
                isActive
                  ? "bg-sentinel-500/10 text-sentinel-500"
                  : "text-muted hover:text-white hover:bg-surface-overlay"
              }`
            }
          >
            <item.icon className="w-5 h-5" />
            {item.label}
          </NavLink>
        ))}
      </nav>

      {/* Footer — version sourced from /api/v1/health */}
      <div className="px-6 py-4 border-t border-border text-xs text-faint">
        Sentinel NVR {version ? `v${version}` : ""}
      </div>
    </aside>
  );
}
