/**
 * Sidebar — responsive navigation sidebar (CG11).
 * Phase 7: shows logged-in username and logout button at the bottom (CG6).
 * Responsive: collapses to hamburger menu on screens < 768px.
 */
import { useEffect, useState } from "react";
import { NavLink, useNavigate, useLocation } from "react-router-dom";
import { Video, Clock, Activity, LayoutDashboard, Camera, Settings, Bell, Shield, LogOut, Users, Upload, Box, Menu, X } from "lucide-react";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";

const navItems = [
  { to: "/live", label: "Live View", icon: Video },
  { to: "/playback", label: "Playback", icon: Clock },
  { to: "/events", label: "Events", icon: Activity },
  { to: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { to: "/cameras", label: "Cameras", icon: Camera },
  { to: "/faces", label: "Faces", icon: Users },
  { to: "/models", label: "Models", icon: Box },
  { to: "/import", label: "Import", icon: Upload, adminOnly: true },
  { to: "/notifications", label: "Notifications", icon: Bell },
  { to: "/settings", label: "Settings", icon: Settings },
];

export default function Sidebar() {
  const [version, setVersion] = useState<string>("");
  const [mobileOpen, setMobileOpen] = useState(false);
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const location = useLocation();

  // Close mobile menu on navigation
  useEffect(() => {
    setMobileOpen(false);
  }, [location.pathname]);

  useEffect(() => {
    const controller = new AbortController();
    api
      .getHealth(controller.signal)
      .then((h) => setVersion(h.version))
      .catch(() => {});
    return () => controller.abort();
  }, []);

  async function handleLogout() {
    await logout();
    navigate("/login", { replace: true });
  }

  const sidebarContent = (
    <>
      {/* Logo */}
      <div className="h-16 flex items-center px-6 border-b border-border">
        <Shield className="w-6 h-6 text-sentinel-500 mr-3" />
        <span className="text-lg font-semibold tracking-tight">Sentinel NVR</span>
        {/* Close button — mobile only */}
        <button
          onClick={() => setMobileOpen(false)}
          className="ml-auto p-1.5 rounded-md text-muted hover:text-white md:hidden"
          aria-label="Close menu"
        >
          <X className="w-5 h-5" />
        </button>
      </div>

      {/* Navigation */}
      <nav className="flex-1 py-4 px-3 space-y-1">
        {navItems.filter((item) => !item.adminOnly || user?.role === "admin").map((item) => (
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

      {/* Footer — username + logout + version */}
      <div className="border-t border-border">
        {user && (
          <div className="px-4 py-3 flex items-center justify-between">
            <span className="text-xs text-muted truncate max-w-[120px]" title={user.username}>
              {user.username}
              {user.role === "admin" && user.role !== user.username && (
                <span className="ml-1.5 text-[10px] text-sentinel-500 font-medium">admin</span>
              )}
            </span>
            <button
              onClick={handleLogout}
              title="Sign out"
              aria-label="Sign out"
              className="p-1.5 rounded-md text-muted hover:text-status-error hover:bg-surface-overlay
                         transition-colors"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        )}
        <div className="px-6 pb-4 text-xs text-faint">
          Sentinel NVR {version ? `v${version}` : ""}
        </div>
      </div>
    </>
  );

  return (
    <>
      {/* Mobile hamburger button — fixed top-left */}
      <button
        onClick={() => setMobileOpen(true)}
        className="fixed top-3 left-3 z-50 p-2 rounded-lg bg-surface-raised border border-border
                   text-muted hover:text-white md:hidden"
        aria-label="Open menu"
      >
        <Menu className="w-5 h-5" />
      </button>

      {/* Desktop sidebar — hidden on mobile */}
      <aside className="hidden md:flex w-64 bg-surface-raised border-r border-border flex-col shrink-0">
        {sidebarContent}
      </aside>

      {/* Mobile sidebar — overlay */}
      {mobileOpen && (
        <>
          {/* Backdrop */}
          <div
            className="fixed inset-0 z-40 bg-black/60 md:hidden"
            onClick={() => setMobileOpen(false)}
          />
          {/* Slide-in panel */}
          <aside className="fixed inset-y-0 left-0 z-50 w-64 bg-surface-raised border-r border-border flex flex-col md:hidden">
            {sidebarContent}
          </aside>
        </>
      )}
    </>
  );
}
