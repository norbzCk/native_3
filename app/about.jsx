import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useAppTheme } from '../context/ThemeContext';

export default function AboutScreen() {
  const { theme } = useAppTheme();

  return (
    <SafeAreaView
      style={[styles.safeArea, { backgroundColor: theme.safeArea }]}
      edges={['left', 'right', 'bottom']}
    >
      <ScrollView contentContainerStyle={styles.content}>
        <View style={[styles.card, { backgroundColor: theme.card, borderColor: theme.border }]}>
          <Text style={[styles.title, { color: theme.text }]}>About Page</Text>
          <Text style={[styles.body, { color: theme.subtext }]}>
            This is the about page. We will put some information about the app here,
            share contact details, and explain what the project is meant to do for users.
          </Text>
          <Text style={[styles.body, { color: theme.subtext }]}>
            Thank you for using our app. We hope you enjoy it and find it useful.
          </Text>
          <Text style={[styles.body, { color: theme.subtext }]}>
            If you have any questions or feedback, please feel free to contact us.
          </Text>
          <Text style={[styles.body, { color: theme.subtext }]}>
            We would love to hear from you.
          </Text>
        </View>
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safeArea: {
    flex: 1,
  },
  content: {
    flexGrow: 1,
    justifyContent: 'center',
    padding: 24,
  },
  card: {
    borderRadius: 18,
    borderWidth: 1,
    padding: 20,
    gap: 12,
  },
  title: {
    fontSize: 28,
    fontWeight: '700',
  },
  body: {
    fontSize: 16,
    lineHeight: 24,
  },
});
