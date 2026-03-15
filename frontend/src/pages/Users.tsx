/**
 * Users — admin-only user management page.
 * Lists all users, allows creating new users, changing roles, changing passwords,
 * and deleting users (cannot delete self).
 */
import { useEffect, useRef, useState } from "react";
import { api, UserRecord } from "../api/client";
import { useAuth } from "../context/AuthContext";
import { Users as UsersIcon } from "lucide-react";
import Toast from "../components/Toast";
import { useToast } from "../hooks/useToast";

export default function Users() {
  const { user: currentUser } = useAuth();
  const [users, setUsers] = useState<UserRecord[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Add user form
  const [newUsername, setNewUsername] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [newRole, setNewRole] = useState<"admin" | "viewer">("viewer");
  const [creating, setCreating] = useState(false);

  // Inline password change
  const [changingPasswordId, setChangingPasswordId] = useState<number | null>(null);
  const [newPwValue, setNewPwValue] = useState("");
  const [changingPw, setChangingPw] = useState(false);

  const { toast, showToast, dismissToast } = useToast();
  const ctrlRef = useRef<AbortController>(null);

  useEffect(() => {
    const controller = new AbortController();
    ctrlRef.current = controller;

    api
      .listUsers(controller.signal)
      .then((u) => {
        if (controller.signal.aborted) return;
        setUsers(u);
        setLoading(false);
      })
      .catch((err) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setError(err.message);
        setLoading(false);
      });

    return () => controller.abort();
  }, []);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    if (creating) return;
    setCreating(true);

    try {
      const created = await api.createUser(newUsername, newPassword, newRole);
      setUsers((prev) => [...prev, created]);
      setNewUsername("");
      setNewPassword("");
      setNewRole("viewer");
      showToast(`User "${created.username}" created`, "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to create user";
      showToast(msg, "error");
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: number, username: string) => {
    if (!window.confirm(`Delete user "${username}"? This cannot be undone.`)) return;
    try {
      await api.deleteUser(id);
      setUsers((prev) => prev.filter((u) => u.id !== id));
      showToast(`User "${username}" deleted`, "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to delete user";
      showToast(msg, "error");
    }
  };

  const handleRoleChange = async (id: number, role: string) => {
    try {
      const updated = await api.updateUserRole(id, role);
      setUsers((prev) => prev.map((u) => (u.id === id ? updated : u)));
      showToast(`Role updated to "${role}"`, "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to update role";
      showToast(msg, "error");
    }
  };

  const handlePasswordChange = async (id: number) => {
    if (changingPw) return;
    if (newPwValue.length < 8) {
      showToast("Password must be at least 8 characters", "error");
      return;
    }
    setChangingPw(true);
    try {
      await api.updateUserPassword(id, newPwValue);
      setChangingPasswordId(null);
      setNewPwValue("");
      showToast("Password updated", "success");
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to update password";
      showToast(msg, "error");
    } finally {
      setChangingPw(false);
    }
  };

  if (loading) {
    return (
      <div className="p-8 text-center py-16">
        <UsersIcon className="w-12 h-12 text-faint mx-auto mb-4" />
        <p className="text-muted animate-pulse">Loading users...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="p-8">
        <h1 className="text-2xl font-semibold mb-6">Users</h1>
        <div className="bg-red-900/20 border border-red-800 rounded-lg p-4">
          <p className="text-red-400 text-sm">Failed to load users: {error}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="p-8 max-w-3xl">
      <h1 className="text-2xl font-semibold mb-6">User Management</h1>

      {/* Add User form */}
      <section className="bg-surface-raised border border-border rounded-lg p-5 mb-6">
        <h2 className="text-sm font-medium text-muted mb-4">Add User</h2>
        <form onSubmit={handleCreate} className="grid grid-cols-1 sm:grid-cols-4 gap-3 items-end">
          <div>
            <label htmlFor="new-username" className="block text-xs text-muted mb-1">
              Username
            </label>
            <input
              id="new-username"
              type="text"
              value={newUsername}
              onChange={(e) => setNewUsername(e.target.value)}
              required
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
              placeholder="username"
            />
          </div>
          <div>
            <label htmlFor="new-password" className="block text-xs text-muted mb-1">
              Password
            </label>
            <input
              id="new-password"
              type="password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              required
              minLength={8}
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
              placeholder="min 8 chars"
            />
          </div>
          <div>
            <label htmlFor="new-role" className="block text-xs text-muted mb-1">
              Role
            </label>
            <select
              id="new-role"
              value={newRole}
              onChange={(e) => setNewRole(e.target.value as "admin" | "viewer")}
              className="w-full bg-surface-base border border-border rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sentinel-500"
            >
              <option value="viewer">viewer</option>
              <option value="admin">admin</option>
            </select>
          </div>
          <button
            type="submit"
            disabled={creating || !newUsername || newPassword.length < 8}
            className="bg-sentinel-500 hover:bg-sentinel-600 disabled:opacity-50 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors"
          >
            {creating ? "Creating..." : "Add User"}
          </button>
        </form>
      </section>

      {/* Users table */}
      <section className="bg-surface-raised border border-border rounded-lg overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-border text-left text-muted">
              <th className="px-4 py-3 font-medium">Username</th>
              <th className="px-4 py-3 font-medium">Role</th>
              <th className="px-4 py-3 font-medium">Created</th>
              <th className="px-4 py-3 font-medium text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.map((u) => {
              const isSelf = currentUser?.id === u.id;
              return (
                <tr key={u.id} className="border-b border-border last:border-b-0 hover:bg-surface-overlay/30">
                  <td className="px-4 py-3 text-white">
                    {u.username}
                    {isSelf && (
                      <span className="ml-2 text-[10px] text-sentinel-500 font-medium">(you)</span>
                    )}
                  </td>
                  <td className="px-4 py-3">
                    <select
                      value={u.role}
                      onChange={(e) => handleRoleChange(u.id, e.target.value)}
                      disabled={isSelf}
                      className="bg-surface-base border border-border rounded px-2 py-1 text-sm text-white focus:outline-none focus:border-sentinel-500 disabled:opacity-50"
                      aria-label={`Change role for ${u.username}`}
                    >
                      <option value="admin">admin</option>
                      <option value="viewer">viewer</option>
                    </select>
                  </td>
                  <td className="px-4 py-3 text-muted">
                    {new Date(u.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-3 text-right space-x-2">
                    {changingPasswordId === u.id ? (
                      <span className="inline-flex items-center gap-2">
                        <input
                          type="password"
                          value={newPwValue}
                          onChange={(e) => setNewPwValue(e.target.value)}
                          placeholder="new password"
                          minLength={8}
                          className="bg-surface-base border border-border rounded px-2 py-1 text-sm text-white w-36 focus:outline-none focus:border-sentinel-500"
                          autoFocus
                          onKeyDown={(e) => {
                            if (e.key === "Escape") {
                              setChangingPasswordId(null);
                              setNewPwValue("");
                            }
                          }}
                        />
                        <button
                          type="button"
                          onClick={() => handlePasswordChange(u.id)}
                          disabled={changingPw || newPwValue.length < 8}
                          className="text-sentinel-400 hover:text-sentinel-300 text-xs disabled:opacity-50"
                        >
                          {changingPw ? "Saving..." : "Save"}
                        </button>
                        <button
                          type="button"
                          onClick={() => { setChangingPasswordId(null); setNewPwValue(""); }}
                          className="text-muted hover:text-white text-xs"
                        >
                          Cancel
                        </button>
                      </span>
                    ) : (
                      <button
                        type="button"
                        onClick={() => { setChangingPasswordId(u.id); setNewPwValue(""); }}
                        className="text-sentinel-400 hover:text-sentinel-300 text-xs"
                      >
                        Change Password
                      </button>
                    )}
                    {!isSelf && (
                      <button
                        type="button"
                        onClick={() => handleDelete(u.id, u.username)}
                        className="text-red-400 hover:text-red-300 text-xs ml-2"
                      >
                        Delete
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
            {users.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-8 text-center text-muted">
                  No users found.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </section>

      {toast && <Toast message={toast.message} type={toast.type} onDismiss={dismissToast} />}
    </div>
  );
}
