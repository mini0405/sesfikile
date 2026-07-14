import { useAuth } from "./context/AuthContext";
import { LoginScreen } from "./screens/LoginScreen";
import { CommuterApp } from "./CommuterApp";

export default function App() {
  const { auth } = useAuth();
  return auth ? <CommuterApp auth={auth} /> : <LoginScreen />;
}
