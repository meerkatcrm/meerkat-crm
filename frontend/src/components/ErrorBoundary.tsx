import { Component, ErrorInfo, ReactNode } from 'react';
import {
  Box,
  Container,
  Typography,
  Button,
  Paper,
  Alert,
  AlertTitle,
} from '@mui/material';
import ErrorOutlineIcon from '@mui/icons-material/ErrorOutlined';
import RefreshIcon from '@mui/icons-material/Refresh';
import HomeIcon from '@mui/icons-material/Home';
import { withTranslation, WithTranslation } from 'react-i18next';

/**
 * Props for ErrorBoundary component
 */
interface ErrorBoundaryProps {
  children: ReactNode;
  /** Optional callback when an error occurs */
  onError?: (error: Error, errorInfo: ErrorInfo) => void;
  /** Whether to show detailed error info (dev mode) */
  showDetails?: boolean;
  /** Component name, for the console log */
  name?: string;
}

/**
 * State for ErrorBoundary component
 */
interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
  errorInfo: ErrorInfo | null;
}

/**
 * ErrorBoundary component
 * 
 * Catches JavaScript errors anywhere in the child component tree,
 * logs those errors, and displays a fallback UI instead of the component
 * tree that crashed.
 * 
 * @example
 * ```tsx
 * <ErrorBoundary name="ContactsList">
 *   <ContactsList />
 * </ErrorBoundary>
 * ```
 */
class ErrorBoundary extends Component<ErrorBoundaryProps & WithTranslation, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps & WithTranslation) {
    super(props);
    this.state = {
      hasError: false,
      error: null,
      errorInfo: null,
    };
  }

  /**
   * Update state when an error is caught
   */
  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return {
      hasError: true,
      error,
    };
  }

  /**
   * Log error information
   */
  componentDidCatch(error: Error, errorInfo: ErrorInfo): void {
    // Log to console
    console.error(`ErrorBoundary${this.props.name ? ` (${this.props.name})` : ''} caught an error:`, error, errorInfo);

    // Update state with error info
    this.setState({
      errorInfo,
    });

    // Call optional error handler
    if (this.props.onError) {
      this.props.onError(error, errorInfo);
    }
  }

  /**
   * Reset error boundary state
   */
  handleReset = (): void => {
    this.setState({
      hasError: false,
      error: null,
      errorInfo: null,
    });
  };

  /**
   * Navigate to home page
   */
  handleGoHome = (): void => {
    window.location.href = '/';
  };

  /**
   * Reload the page
   */
  handleReload = (): void => {
    window.location.reload();
  };

  render(): ReactNode {
    const { hasError, error, errorInfo } = this.state;
    const { children, showDetails, t } = this.props;

    if (hasError) {
      return (
        <Container maxWidth="md">
          <Box
            sx={{
              minHeight: '100vh',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              py: 4,
            }}
          >
            <Paper
              elevation={3}
              sx={{
                p: 4,
                textAlign: 'center',
                width: '100%',
              }}
            >
              <ErrorOutlineIcon
                sx={{
                  fontSize: 64,
                  color: 'error.main',
                  mb: 2,
                }}
              />
              
              <Typography variant="h4" gutterBottom>
                {t('errorBoundary.title')}
              </Typography>
              
              <Typography
                variant="body1"
                sx={{
                  color: "text.secondary",
                  marginBottom: "16px"
                }}>
                {t('errorBoundary.message')}
              </Typography>

              <Box sx={{ mt: 3, display: 'flex', gap: 2, justifyContent: 'center' }}>
                <Button
                  variant="contained"
                  startIcon={<RefreshIcon />}
                  onClick={this.handleReset}
                >
                  {t('errorBoundary.tryAgain')}
                </Button>
                <Button
                  variant="outlined"
                  startIcon={<RefreshIcon />}
                  onClick={this.handleReload}
                >
                  {t('errorBoundary.reloadPage')}
                </Button>
                <Button
                  variant="outlined"
                  startIcon={<HomeIcon />}
                  onClick={this.handleGoHome}
                >
                  {t('errorBoundary.goHome')}
                </Button>
              </Box>

              {/* Show error details in development */}
              {showDetails && error && (
                <Alert severity="error" sx={{ mt: 3, textAlign: 'left' }}>
                  <AlertTitle>{t('errorBoundary.detailsTitle')}</AlertTitle>
                  <Typography variant="body2" component="pre" sx={{ whiteSpace: 'pre-wrap', fontSize: '0.75rem' }}>
                    <strong>{t('errorBoundary.error')}</strong> {error.toString()}
                    {'\n\n'}
                    <strong>{t('errorBoundary.componentStack')}</strong>
                    {errorInfo?.componentStack}
                  </Typography>
                </Alert>
              )}
            </Paper>
          </Box>
        </Container>
      );
    }

    return children;
  }
}

export default withTranslation()(ErrorBoundary);
