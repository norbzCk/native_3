import { Link } from 'expo-router';
import { LinearGradient } from 'expo-linear-gradient';
import { Image, Pressable, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useAppTheme } from '../context/ThemeContext';

export default function HomeScreen() {
  const { theme, themeName, toggleTheme } = useAppTheme();
  const nextThemeName = themeName === 'light' ? 'dark' : 'light';

  return (
    <SafeAreaView
      style={[styles.safeArea, { backgroundColor: theme.safeArea }]}
      edges={['left', 'right', 'bottom']}
    >
      <LinearGradient
        colors={theme.gradient}
        start={{ x: 0, y: 0 }}
        end={{ x: 1, y: 1 }}
        style={styles.container}
      >
        <View style={styles.hero}>
          <Text style={[styles.title, { color: theme.text }]}>Hello Norbs</Text>
          <Text style={[styles.subtitle, { color: theme.subtext }]}>
            It's time for mobile app development!
          </Text>
          <View style={styles.toggleWrap}>
            <Text style={[styles.modeLabel, { color: theme.subtext }]}>
              Current mode: {themeName === 'light' ? 'Light' : 'Dark'}
            </Text>
            <Pressable
              onPress={toggleTheme}
              style={({ pressed }) => [
                styles.themeButton,
                {
                  backgroundColor: theme.toggleButtonBackground,
                  borderColor: theme.toggleButtonBorder,
                  opacity: pressed ? 0.85 : 1,
                },
              ]}
            >
              <Text style={[styles.themeButtonText, { color: theme.toggleButtonText }]}>
                Switch to {nextThemeName === 'light' ? 'Light' : 'Dark'} Mode
              </Text>
            </Pressable>
          </View>
        </View>

        <View style={[styles.card, { backgroundColor: theme.card, borderColor: theme.border }]}>
          <Text style={[styles.cardText, { color: theme.cardText }]}>Welcome dev</Text>
          <Image
            source={require('../assets/icon.png')}
            style={styles.image}
            resizeMode="contain"
          />

          <Link href="/about" style={[styles.linkText, { color: theme.link }]}>
            About
          </Link>
        </View>
      </LinearGradient>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
  },
  container: {
    flex: 1,
    paddingHorizontal: 24,
    paddingTop: 24,
    paddingBottom: 24,
    justifyContent: 'space-between',
    alignItems: 'center',
  },
  hero: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  title: {
    fontSize: 28,
    fontWeight: '700',
    textAlign: 'center',
  },
  subtitle: {
    marginTop: 8,
    fontSize: 16,
    textAlign: 'center',
  },
  toggleWrap: {
    marginTop: 28,
    alignItems: 'center',
    gap: 12,
  },
  modeLabel: {
    fontSize: 15,
    fontWeight: '500',
  },
  themeButton: {
    minWidth: 190,
    borderRadius: 999,
    borderWidth: 1,
    paddingVertical: 12,
    paddingHorizontal: 20,
    alignItems: 'center',
  },
  themeButtonText: {
    fontSize: 15,
    fontWeight: '700',
  },
  card: {
    width: '80%',
    borderRadius: 12,
    borderWidth: 1,
    paddingVertical: 12,
    paddingHorizontal: 16,
    alignItems: 'center',
  },
  cardText: {
    fontSize: 16,
    fontWeight: '600',
  },
  image: {
    width: 96,
    height: 96,
    marginTop: 12,
  },
  linkText: {
    marginTop: 12,
    fontSize: 16,
    fontWeight: '600',
  },
});
