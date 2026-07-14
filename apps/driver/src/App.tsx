import { useAuth } from "./context/AuthContext";
import { LoginScreen } from "./screens/LoginScreen";
import { DriverApp } from "./DriverApp";

export default function App() {
  const { auth } = useAuth();
  return auth ? <DriverApp auth={auth} /> : <LoginScreen />;
}
